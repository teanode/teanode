package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/pending"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

// ConnectedTarget describes a Chrome tab attached via a backend.
type ConnectedTarget struct {
	SessionID string
	TargetID  string
	URL       string
	Title     string
	Source    string // "extension" or "headless"
}

// Relay manages the WebSocket connection from the Chrome extension.
type Relay struct {
	connection *websocket.Conn
	targets    map[string]*ConnectedTarget // sessionID -> target
	pending    *pending.Requests
	mutex      sync.Mutex
	done       chan struct{} // closed when current connection closes
}

// NewRelay creates a new relay (no connection yet).
func NewRelay() *Relay {
	return &Relay{
		targets: make(map[string]*ConnectedTarget),
		pending: pending.NewRequests(),
	}
}

// HandleWebSocket upgrades the HTTP connection and manages the extension link.
func (self *Relay) HandleWebSocket(writer http.ResponseWriter, request *http.Request) {
	connection, err := wsUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("relay: upgrade error: %v", err)
		return
	}

	self.mutex.Lock()
	// Replace any existing connection.
	if self.connection != nil {
		self.connection.Close()
	}
	self.connection = connection
	self.targets = make(map[string]*ConnectedTarget)
	self.pending.RejectAll("connection replaced")
	self.done = make(chan struct{})
	done := self.done
	self.mutex.Unlock()

	log.Info("relay: extension connected")

	go self.pingLoop(connection, done)
	self.readLoop(connection, done)
}

// Connected reports whether the extension is connected.
func (self *Relay) Connected() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.connection != nil
}

// Targets returns a snapshot of connected targets.
func (self *Relay) Targets() []ConnectedTarget {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	out := make([]ConnectedTarget, 0, len(self.targets))
	for _, target := range self.targets {
		out = append(out, *target)
	}
	return out
}

// DefaultTarget returns the first connected target, or an error.
func (self *Relay) DefaultTarget() (*ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, target := range self.targets {
		return target, nil
	}
	return nil, errors.New("no attached browser tab")
}

// TargetByConnectionId looks up a target by its session ID (used as connectionId).
func (self *Relay) TargetByConnectionId(connectionId string) (*ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	target, ok := self.targets[connectionId]
	if !ok {
		return nil, fmt.Errorf("browser connection %q not found", connectionId)
	}
	return target, nil
}

// SendCDPCommand sends a CDP command through the extension and waits for the result.
func (self *Relay) SendCDPCommand(ctx context.Context, method string, parameters interface{}, sessionId string) (json.RawMessage, error) {
	self.mutex.Lock()
	if self.connection == nil {
		self.mutex.Unlock()
		return nil, errors.New("browser extension not connected")
	}
	commandId, resultChannel := self.pending.Allocate()
	connection := self.connection
	self.mutex.Unlock()

	message := map[string]interface{}{
		"id":     commandId,
		"method": "forwardCDPCommand",
		"params": map[string]interface{}{
			"method":    method,
			"params":    parameters,
			"sessionId": sessionId,
		},
	}
	data, err := json.Marshal(message)
	if err != nil {
		self.pending.Cancel(commandId)
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if err := connection.WriteMessage(websocket.TextMessage, data); err != nil {
		self.pending.Cancel(commandId)
		return nil, fmt.Errorf("write: %w", err)
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

func (self *Relay) readLoop(connection *websocket.Conn, done chan struct{}) {
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
			Error  *string         `json:"error"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}

		// Pong from extension — ignore.
		if frame.Method == "pong" {
			continue
		}

		// Response to a pending command.
		if frame.ID != nil && (frame.Result != nil || frame.Error != nil) {
			result := pending.Result{Data: frame.Result}
			if frame.Error != nil {
				result.Error = *frame.Error
			}
			self.pending.Resolve(*frame.ID, result)
			continue
		}

		// CDP event from extension.
		if frame.Method == "forwardCDPEvent" && frame.Params != nil {
			self.handleCDPEvent(frame.Params)
			continue
		}
	}
}

func (self *Relay) handleCDPEvent(raw json.RawMessage) {
	var event struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return
	}

	switch event.Method {
	case "Target.attachedToTarget":
		var payload struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(event.Params, &payload) == nil && payload.SessionID != "" {
			self.mutex.Lock()
			self.targets[payload.SessionID] = &ConnectedTarget{
				SessionID: payload.SessionID,
				TargetID:  payload.TargetInfo.TargetID,
				URL:       payload.TargetInfo.URL,
				Title:     payload.TargetInfo.Title,
				Source:    "extension",
			}
			self.mutex.Unlock()
			log.Infof("relay: target attached session=%s url=%s", payload.SessionID, payload.TargetInfo.URL)
		}

	case "Target.detachedFromTarget":
		var payload struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(event.Params, &payload) == nil && payload.SessionID != "" {
			self.mutex.Lock()
			delete(self.targets, payload.SessionID)
			self.mutex.Unlock()
			log.Infof("relay: target detached session=%s", payload.SessionID)
		}

	case "Target.targetInfoChanged":
		var payload struct {
			TargetInfo struct {
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
				Title    string `json:"title"`
			} `json:"targetInfo"`
		}
		if json.Unmarshal(event.Params, &payload) == nil {
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
	}
}

func (self *Relay) pingLoop(connection *websocket.Conn, done chan struct{}) {
	defer deferutil.Recover()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			pingMessage, _ := json.Marshal(map[string]string{"method": "ping"})
			if err := connection.WriteMessage(websocket.TextMessage, pingMessage); err != nil {
				return
			}
		}
	}
}

func (self *Relay) onDisconnect(connection *websocket.Conn) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Only clean up if this is still the active connection.
	if self.connection != connection {
		return
	}
	self.connection = nil
	self.targets = make(map[string]*ConnectedTarget)
	self.pending.RejectAll("extension disconnected")

	log.Info("relay: extension disconnected")
}
