package relaybrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/pending"
)

var log = logging.MustGetLogger("relaybrowser")

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

// relayConnection holds the state for a single extension WebSocket connection.
type relayConnection struct {
	userId     string
	connection *websocket.Conn
	targets    map[string]*browsers.ConnectedTarget // sessionId -> target
	pending    *pending.Requests
	done       chan struct{}
}

// Relay manages WebSocket connections from Chrome extensions.
type Relay struct {
	connections    map[string]*relayConnection // connectionId -> connection
	nextConnection int
	mutex          sync.Mutex
}

// NewRelay creates a new relay (no connections yet).
func NewRelay() *Relay {
	return &Relay{
		connections: make(map[string]*relayConnection),
	}
}

// HandleWebSocketForUser upgrades and binds a browser extension connection to one user.
func (self *Relay) HandleWebSocketForUser(writer http.ResponseWriter, request *http.Request, userId string) {
	userId = strings.TrimSpace(userId)
	if userId == "" {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := wsUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("upgrade error: %v", err)
		return
	}

	self.mutex.Lock()
	self.nextConnection++
	connectionId := strconv.Itoa(self.nextConnection)
	rc := &relayConnection{
		userId:     userId,
		connection: conn,
		targets:    make(map[string]*browsers.ConnectedTarget),
		pending:    pending.NewRequests(),
		done:       make(chan struct{}),
	}
	self.connections[connectionId] = rc
	done := rc.done
	self.mutex.Unlock()

	log.Infof("extension connected user=%s id=%s", userId, connectionId)

	go self.pingLoop(connectionId, conn, done)
	self.readLoop(connectionId, conn, done)
}

// Connected reports whether at least one extension is connected.
func (self *Relay) Connected() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.connections) > 0
}

// Targets returns a snapshot of connected targets from all connections.
func (self *Relay) Targets() []browsers.ConnectedTarget {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var out []browsers.ConnectedTarget
	for _, rc := range self.connections {
		for _, target := range rc.targets {
			out = append(out, *target)
		}
	}
	return out
}

// TargetsForUser returns a snapshot of connected targets for one user.
func (self *Relay) TargetsForUser(userId string) []browsers.ConnectedTarget {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var out []browsers.ConnectedTarget
	for _, rc := range self.connections {
		if rc.userId != userId {
			continue
		}
		for _, target := range rc.targets {
			out = append(out, *target)
		}
	}
	return out
}

// DefaultTarget returns the first connected target, or an error.
func (self *Relay) DefaultTarget() (*browsers.ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, rc := range self.connections {
		for _, target := range rc.targets {
			return target, nil
		}
	}
	return nil, errors.New("no attached browser tab")
}

// DefaultTargetForUser returns the first connected target for userId.
func (self *Relay) DefaultTargetForUser(userId string) (*browsers.ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, rc := range self.connections {
		if rc.userId != userId {
			continue
		}
		for _, target := range rc.targets {
			return target, nil
		}
	}
	return nil, errors.New("no attached browser tab")
}

// TargetByConnectionID looks up a target by its session ID (used as connectionId).
func (self *Relay) TargetByConnectionID(connectionId string) (*browsers.ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, rc := range self.connections {
		if target, ok := rc.targets[connectionId]; ok {
			return target, nil
		}
	}
	return nil, fmt.Errorf("browser connection %q not found", connectionId)
}

// TargetByConnectionIDForUser looks up a target by session ID for a specific user.
func (self *Relay) TargetByConnectionIDForUser(userId, connectionId string) (*browsers.ConnectedTarget, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, rc := range self.connections {
		if rc.userId != userId {
			continue
		}
		if target, ok := rc.targets[connectionId]; ok {
			return target, nil
		}
	}
	return nil, fmt.Errorf("browser connection %q not found", connectionId)
}

// findConnectionForSession returns the relayConnection that owns the given sessionId.
// Must be called with self.mutex held.
func (self *Relay) findConnectionForSession(sessionId string) *relayConnection {
	for _, rc := range self.connections {
		if _, ok := rc.targets[sessionId]; ok {
			return rc
		}
	}
	return nil
}

// SendCDPCommand sends a CDP command through the extension and waits for the result.
func (self *Relay) SendCDPCommand(ctx context.Context, method string, parameters interface{}, sessionId string) (json.RawMessage, error) {
	self.mutex.Lock()
	rc := self.findConnectionForSession(sessionId)
	if rc == nil {
		self.mutex.Unlock()
		return nil, errors.New("browser extension not connected")
	}
	commandId, resultChannel := rc.pending.Allocate()
	conn := rc.connection
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
		rc.pending.Cancel(commandId)
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		rc.pending.Cancel(commandId)
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case <-ctx.Done():
		rc.pending.Cancel(commandId)
		return nil, ctx.Err()
	case result := <-resultChannel:
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return result.Data, nil
	}
}

func (self *Relay) readLoop(connectionId string, connection *websocket.Conn, done chan struct{}) {
	defer func() {
		self.onDisconnect(connectionId, connection)
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
			self.mutex.Lock()
			rc, ok := self.connections[connectionId]
			self.mutex.Unlock()
			if ok {
				result := pending.Result{Data: frame.Result}
				if frame.Error != nil {
					result.Error = *frame.Error
				}
				rc.pending.Resolve(*frame.ID, result)
			}
			continue
		}

		// CDP event from extension.
		if frame.Method == "forwardCDPEvent" && frame.Params != nil {
			self.handleCDPEvent(connectionId, frame.Params)
			continue
		}
	}
}

func (self *Relay) handleCDPEvent(connectionId string, raw json.RawMessage) {
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
			if rc, ok := self.connections[connectionId]; ok {
				rc.targets[payload.SessionID] = &browsers.ConnectedTarget{
					SessionID: payload.SessionID,
					TargetID:  payload.TargetInfo.TargetID,
					URL:       payload.TargetInfo.URL,
					Title:     payload.TargetInfo.Title,
					Source:    "extension",
				}
			}
			self.mutex.Unlock()
			log.Infof("target attached session=%s url=%s", payload.SessionID, payload.TargetInfo.URL)
		}

	case "Target.detachedFromTarget":
		var payload struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(event.Params, &payload) == nil && payload.SessionID != "" {
			self.mutex.Lock()
			if rc, ok := self.connections[connectionId]; ok {
				delete(rc.targets, payload.SessionID)
			}
			self.mutex.Unlock()
			log.Infof("target detached session=%s", payload.SessionID)
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
			if rc, ok := self.connections[connectionId]; ok {
				for _, target := range rc.targets {
					if target.TargetID == payload.TargetInfo.TargetID {
						target.URL = payload.TargetInfo.URL
						target.Title = payload.TargetInfo.Title
						break
					}
				}
			}
			self.mutex.Unlock()
		}
	}
}

func (self *Relay) pingLoop(connectionId string, connection *websocket.Conn, done chan struct{}) {
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

func (self *Relay) onDisconnect(connectionId string, connection *websocket.Conn) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	rc, ok := self.connections[connectionId]
	if !ok || rc.connection != connection {
		return
	}

	rc.pending.RejectAll("extension disconnected")
	delete(self.connections, connectionId)

	log.Infof("extension disconnected id=%s", connectionId)
}
