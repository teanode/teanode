package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/pending"
)

// Headless connects directly to a CDP endpoint (e.g. chromedp/headless-shell
// on 127.0.0.1:9222) and implements the Browser interface.
type Headless struct {
	endpoint      string // host:port of the CDP debugger
	connection    *websocket.Conn
	targets       map[string]*ConnectedTarget // sessionID -> target
	pending       *pending.Requests
	mutex         sync.Mutex
	writeMutex    sync.Mutex // serializes WebSocket writes (gorilla requires this)
	done          chan struct{}
	stopReconnect chan struct{}
}

// NewHeadless creates a new headless browser client for the given endpoint.
func NewHeadless(endpoint string) *Headless {
	return &Headless{
		endpoint: endpoint,
		targets:  make(map[string]*ConnectedTarget),
		pending:  pending.NewRequests(),
	}
}

// Connect discovers the browser WebSocket URL, dials it, attaches to existing
// page targets, and starts the read loop for ongoing events.
func (self *Headless) Connect(ctx context.Context) error {
	// 1. GET /json/version to find the WebSocket debugger URL.
	versionURL := fmt.Sprintf("http://%s/json/version", self.endpoint)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return fmt.Errorf("headless: creating request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("headless: fetching %s: %w", versionURL, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("headless: reading version response: %w", err)
	}

	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &versionInfo); err != nil {
		return fmt.Errorf("headless: parsing version response: %w", err)
	}
	if versionInfo.WebSocketDebuggerURL == "" {
		return fmt.Errorf("headless: no webSocketDebuggerUrl in version response")
	}

	// 2. Dial the browser-level WebSocket.
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, versionInfo.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("headless: dialing %s: %w", versionInfo.WebSocketDebuggerURL, err)
	}

	self.mutex.Lock()
	self.connection = connection
	self.done = make(chan struct{})
	done := self.done
	self.mutex.Unlock()

	// Start the read loop before sending commands so we don't miss events.
	go self.readLoop(connection, done)

	// 3. List existing targets and auto-attach to page targets.
	// We do this before enabling discovery to avoid races between targetCreated
	// event handlers and the manual attach loop below.
	targetResult, err := self.sendBrowserCommand(ctx, "Target.getTargets", nil)
	if err != nil {
		connection.Close()
		return fmt.Errorf("headless: getTargets: %w", err)
	}

	var targetList struct {
		TargetInfos []targetInfo `json:"targetInfos"`
	}
	if err := json.Unmarshal(targetResult, &targetList); err == nil {
		log.Infof("headless: discovered %d targets", len(targetList.TargetInfos))
		for _, info := range targetList.TargetInfos {
			log.Infof("headless:   target %s type=%s url=%s", info.TargetID, info.Type, info.URL)
			if info.Type == "page" {
				self.attachTarget(ctx, info)
			}
		}
	}

	// If no page targets exist, create one so browser tools have
	// something to work with. chromedp/headless-shell can start empty.
	self.mutex.Lock()
	hasTargets := len(self.targets) > 0
	self.mutex.Unlock()

	if !hasTargets {
		log.Info("headless: no page targets found, creating one")
		createResult, err := self.sendBrowserCommand(ctx, "Target.createTarget", map[string]interface{}{
			"url": "about:blank",
		})
		if err != nil {
			log.Errorf("headless: createTarget: %v", err)
		} else {
			var created struct {
				TargetID string `json:"targetId"`
			}
			if json.Unmarshal(createResult, &created) == nil && created.TargetID != "" {
				self.attachTarget(ctx, targetInfo{
					TargetID: created.TargetID,
					Type:     "page",
					URL:      "about:blank",
				})
			}
		}
	}

	// 4. Enable target discovery so we receive targetCreated events for
	// targets created after this point.
	_, err = self.sendBrowserCommand(ctx, "Target.setDiscoverTargets", map[string]interface{}{
		"discover": true,
	})
	if err != nil {
		connection.Close()
		return fmt.Errorf("headless: setDiscoverTargets: %w", err)
	}

	log.Infof("headless: connected to %s", self.endpoint)
	return nil
}

// Close tears down the WebSocket connection and stops any reconnect loop.
func (self *Headless) Close() {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Stop the reconnect loop first so it doesn't restart after we close.
	if self.stopReconnect != nil {
		select {
		case <-self.stopReconnect:
		default:
			close(self.stopReconnect)
		}
	}

	if self.connection != nil {
		self.connection.Close()
		self.connection = nil
	}
	self.targets = make(map[string]*ConnectedTarget)
	self.pending.RejectAll("headless connection closed")
}

// StartReconnectLoop spawns a goroutine that re-establishes the CDP
// connection whenever it drops, using exponential backoff. Call Close()
// to stop the loop.
func (self *Headless) StartReconnectLoop(ctx context.Context) {
	self.mutex.Lock()
	self.stopReconnect = make(chan struct{})
	done := self.done
	if done == nil {
		// Not currently connected (initial Connect failed or was never
		// called) — use an already-closed channel to trigger an immediate
		// reconnect attempt.
		done = make(chan struct{})
		close(done)
	}
	self.mutex.Unlock()

	go self.reconnectLoop(ctx, done)
}

func (self *Headless) reconnectLoop(ctx context.Context, done chan struct{}) {
	defer deferutil.Recover()

	for {
		// Wait for the current connection to drop.
		select {
		case <-done:
		case <-self.stopReconnect:
			return
		case <-ctx.Done():
			return
		}

		delay := time.Second
		maxDelay := 30 * time.Second

		for {
			select {
			case <-self.stopReconnect:
				return
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			log.Infof("headless: reconnecting to %s", self.endpoint)
			connectContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := self.Connect(connectContext)
			cancel()

			if err != nil {
				log.Errorf("headless: reconnect failed: %v", err)
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
				continue
			}

			// Success — grab the new done channel for the next iteration.
			self.mutex.Lock()
			done = self.done
			self.mutex.Unlock()
			break
		}
	}
}

// Connected reports whether the headless browser connection is active.
func (self *Headless) Connected() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.connection != nil
}

// Targets returns a snapshot of connected targets.
func (self *Headless) Targets() []ConnectedTarget {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	out := make([]ConnectedTarget, 0, len(self.targets))
	for _, target := range self.targets {
		out = append(out, *target)
	}
	return out
}

// DefaultTarget returns the first connected target, or an error.
func (self *Headless) DefaultTarget() (*ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, target := range self.targets {
		return target, nil
	}
	return nil, errors.New("no attached browser tab")
}

// TargetByConnectionId looks up a target by its session ID.
func (self *Headless) TargetByConnectionId(connectionId string) (*ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	target, ok := self.targets[connectionId]
	if !ok {
		return nil, fmt.Errorf("browser connection %q not found", connectionId)
	}
	return target, nil
}

// SendCDPCommand sends a CDP command to a specific target session.
func (self *Headless) SendCDPCommand(ctx context.Context, method string, parameters interface{}, sessionId string) (json.RawMessage, error) {
	self.mutex.Lock()
	if self.connection == nil {
		self.mutex.Unlock()
		return nil, errors.New("headless browser not connected")
	}
	commandId, resultChannel := self.pending.Allocate()
	connection := self.connection
	self.mutex.Unlock()

	message := map[string]interface{}{
		"id":        commandId,
		"method":    method,
		"sessionId": sessionId,
	}
	if parameters != nil {
		message["params"] = parameters
	}

	if err := self.writeJSON(connection, message); err != nil {
		self.pending.Cancel(commandId)
		return nil, err
	}

	select {
	case <-ctx.Done():
		self.pending.Cancel(commandId)
		return nil, ctx.Err()
	case result := <-resultChannel:
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return result.Data, nil
	}
}

// sendBrowserCommand sends a CDP command at the browser level (no sessionId).
func (self *Headless) sendBrowserCommand(ctx context.Context, method string, parameters interface{}) (json.RawMessage, error) {
	self.mutex.Lock()
	if self.connection == nil {
		self.mutex.Unlock()
		return nil, errors.New("headless browser not connected")
	}
	commandId, resultChannel := self.pending.Allocate()
	connection := self.connection
	self.mutex.Unlock()

	message := map[string]interface{}{
		"id":     commandId,
		"method": method,
	}
	if parameters != nil {
		message["params"] = parameters
	}

	if err := self.writeJSON(connection, message); err != nil {
		self.pending.Cancel(commandId)
		return nil, err
	}

	select {
	case <-ctx.Done():
		self.pending.Cancel(commandId)
		return nil, ctx.Err()
	case result := <-resultChannel:
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return result.Data, nil
	}
}

// writeJSON serializes message as JSON and writes it to the WebSocket.
// All writes are serialized through writeMutex since gorilla/websocket
// does not support concurrent writers.
func (self *Headless) writeJSON(connection *websocket.Conn, message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	self.writeMutex.Lock()
	err = connection.WriteMessage(websocket.TextMessage, data)
	self.writeMutex.Unlock()

	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// targetInfo holds the fields we care about from CDP Target.TargetInfo.
type targetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

// attachTarget attaches to a target in flatten mode and registers it directly.
func (self *Headless) attachTarget(ctx context.Context, info targetInfo) {
	result, err := self.sendBrowserCommand(ctx, "Target.attachToTarget", map[string]interface{}{
		"targetId": info.TargetID,
		"flatten":  true,
	})
	if err != nil {
		log.Errorf("headless: attachToTarget %s: %v", info.TargetID, err)
		return
	}

	var attachResponse struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &attachResponse); err != nil || attachResponse.SessionID == "" {
		return
	}

	// Register the target immediately rather than relying on the
	// Target.attachedToTarget event which may arrive out of order or
	// be missed during concurrent operations.
	self.mutex.Lock()
	self.targets[attachResponse.SessionID] = &ConnectedTarget{
		SessionID: attachResponse.SessionID,
		TargetID:  info.TargetID,
		URL:       info.URL,
		Title:     info.Title,
		Source:    "headless",
	}
	self.mutex.Unlock()

	log.Infof("headless: attached to target %s session=%s url=%s", info.TargetID, attachResponse.SessionID, info.URL)
}

func (self *Headless) readLoop(connection *websocket.Conn, done chan struct{}) {
	defer deferutil.Recover()
	defer func() {
		self.onDisconnect(connection)
		close(done)
	}()

	for {
		_, data, err := connection.ReadMessage()
		if err != nil {
			return
		}

		var frame struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}

		// Response to a pending command.
		if frame.ID != nil {
			result := pending.Result{Data: frame.Result}
			if frame.Error != nil {
				result.Error = frame.Error.Message
			}
			self.pending.Resolve(*frame.ID, result)
			continue
		}

		// CDP event — handle target lifecycle events.
		if frame.Method != "" {
			self.handleEvent(frame.Method, frame.Params)
		}
	}
}

func (self *Headless) handleEvent(method string, params json.RawMessage) {
	switch method {
	case "Target.attachedToTarget":
		var payload struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				TargetID string `json:"targetId"`
				Type     string `json:"type"`
				URL      string `json:"url"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(params, &payload) == nil && payload.SessionID != "" {
			self.mutex.Lock()
			// Only store if not already registered by attachTarget.
			if _, exists := self.targets[payload.SessionID]; !exists {
				self.targets[payload.SessionID] = &ConnectedTarget{
					SessionID: payload.SessionID,
					TargetID:  payload.TargetInfo.TargetID,
					URL:       payload.TargetInfo.URL,
					Title:     payload.TargetInfo.Title,
					Source:    "headless",
				}
				log.Infof("headless: target attached (event) session=%s url=%s", payload.SessionID, payload.TargetInfo.URL)
			}
			self.mutex.Unlock()
		}

	case "Target.detachedFromTarget":
		var payload struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(params, &payload) == nil && payload.SessionID != "" {
			self.mutex.Lock()
			delete(self.targets, payload.SessionID)
			self.mutex.Unlock()
			log.Infof("headless: target detached session=%s", payload.SessionID)
		}

	case "Target.targetInfoChanged":
		var payload struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(params, &payload) == nil {
			self.mutex.Lock()
			for _, target := range self.targets {
				if target.TargetID == payload.TargetInfo.TargetID {
					target.URL = payload.TargetInfo.URL
					target.Title = payload.TargetInfo.Title
					break
				}
			}
			self.mutex.Unlock()
		}

	case "Target.targetCreated":
		// Auto-attach to new page targets created after Connect().
		var payload struct {
			TargetInfo targetInfo `json:"targetInfo"`
		}
		if json.Unmarshal(params, &payload) == nil && payload.TargetInfo.Type == "page" {
			// Check if we already have this target.
			self.mutex.Lock()
			alreadyAttached := false
			for _, target := range self.targets {
				if target.TargetID == payload.TargetInfo.TargetID {
					alreadyAttached = true
					break
				}
			}
			self.mutex.Unlock()

			if !alreadyAttached {
				go self.attachTarget(context.Background(), payload.TargetInfo)
			}
		}
	}
}

func (self *Headless) onDisconnect(connection *websocket.Conn) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.connection != connection {
		return
	}
	self.connection = nil
	self.targets = make(map[string]*ConnectedTarget)
	self.pending.RejectAll("headless connection lost")

	log.Info("headless: disconnected")
}
