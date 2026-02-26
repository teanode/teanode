package terminals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/pending"
	"github.com/teanode/teanode/internal/web"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

// terminalConnection holds the state for a single named terminal connection.
type terminalConnection struct {
	id         string
	userId     string
	connection *websocket.Conn
	pending    *pending.Requests
	done       chan struct{}
	machine    MachineInfo
}

// MachineInfo holds metadata sent by the terminal client on attach.
type MachineInfo struct {
	Hostname         string `json:"hostname,omitempty"`
	Username         string `json:"username,omitempty"`
	OS               string `json:"os,omitempty"`
	Architecture     string `json:"architecture,omitempty"`
	ShellCommand     string `json:"shellCommand,omitempty"`
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	Timezone         string `json:"timezone,omitempty"`
}

// ConnectionInfo describes a connected terminal for listing purposes.
type ConnectionInfo struct {
	ID      string
	Machine MachineInfo
}

// Relay manages WebSocket connections from terminal CLI clients.
type Relay struct {
	connections map[string]*terminalConnection
	mutex       sync.Mutex
}

// NewRelay creates a new terminal relay (no connections yet).
func NewRelay() *Relay {
	return &Relay{
		connections: make(map[string]*terminalConnection),
	}
}

// HandleWebSocket upgrades and binds a terminal connection to one user.
func (self *Relay) HandleWebSocket(writer http.ResponseWriter, request *http.Request) error {
	id := request.URL.Query().Get("id")
	if id == "" {
		return web.Error(http.StatusBadRequest, "missing terminal connection id")
	}

	websocketConnection, err := wsUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("terminal: upgrade error: %v", err)
		return err
	}

	user := models.UserFromContext(request.Context())

	connectionKey := user.ID + ":" + id
	self.mutex.Lock()
	// If a connection with this user+id already exists, replace it.
	if existing, ok := self.connections[connectionKey]; ok {
		existing.connection.Close()
		existing.pending.RejectAll("connection replaced")
	}
	terminal := &terminalConnection{
		id:         id,
		userId:     user.ID,
		connection: websocketConnection,
		pending:    pending.NewRequests(),
		done:       make(chan struct{}),
	}
	self.connections[connectionKey] = terminal
	done := terminal.done
	self.mutex.Unlock()

	log.Infof("terminal: client connected user=%s id=%s", user.ID, id)

	go self.pingLoop(connectionKey, websocketConnection, done)
	self.readLoop(connectionKey, websocketConnection, done)
	return nil
}

// Connected reports whether at least one terminal client is connected.
func (self *Relay) Connected() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.connections) > 0
}

// ConnectionsForUser returns the caller's terminal connections.
func (self *Relay) ConnectionsForUser(userId string) []ConnectionInfo {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	out := make([]ConnectionInfo, 0)
	for _, tc := range self.connections {
		if tc.userId != userId {
			continue
		}
		out = append(out, ConnectionInfo{ID: tc.id, Machine: tc.machine})
	}
	return out
}

// DefaultConnectionForUser returns the first connection for the given user.
func (self *Relay) DefaultConnectionForUser(userId string) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, tc := range self.connections {
		if tc.userId == userId {
			return tc.id, nil
		}
	}
	return "", errors.New("terminal client not connected")
}

// SendCommandForUser sends a command, enforcing that connectionId belongs to userId.
func (self *Relay) SendCommandForUser(ctx context.Context, userId, connectionId string, method string, parameters interface{}) (json.RawMessage, error) {
	self.mutex.Lock()
	var terminal *terminalConnection
	for _, candidate := range self.connections {
		if candidate.userId == userId && candidate.id == connectionId {
			terminal = candidate
			break
		}
	}
	if terminal == nil {
		self.mutex.Unlock()
		return nil, fmt.Errorf("terminal connection %q not found", connectionId)
	}
	commandId, resultChannel := terminal.pending.Allocate()
	connection := terminal.connection
	self.mutex.Unlock()

	message := map[string]interface{}{
		"id":     commandId,
		"method": method,
	}
	if parameters != nil {
		message["params"] = parameters
	}
	data, err := json.Marshal(message)
	if err != nil {
		terminal.pending.Cancel(commandId)
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if err := connection.WriteMessage(websocket.TextMessage, data); err != nil {
		terminal.pending.Cancel(commandId)
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case <-ctx.Done():
		terminal.pending.Cancel(commandId)
		return nil, ctx.Err()
	case result := <-resultChannel:
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return result.Data, nil
	}
}

func (self *Relay) readLoop(connectionKey string, connection *websocket.Conn, done chan struct{}) {
	defer func() {
		self.onDisconnect(connectionKey, connection)
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

		if frame.Method == "pong" {
			continue
		}

		// Client sends machine info on connect.
		if frame.Method == "attach" && frame.Params != nil {
			var info MachineInfo
			if json.Unmarshal(frame.Params, &info) == nil {
				self.mutex.Lock()
				if tc, ok := self.connections[connectionKey]; ok {
					tc.machine = info
				}
				self.mutex.Unlock()
				log.Infof("terminal: attach key=%s host=%s user=%s tz=%s", connectionKey, info.Hostname, info.Username, info.Timezone)
			}
			continue
		}

		if frame.ID != nil && (frame.Result != nil || frame.Error != nil) {
			self.mutex.Lock()
			terminal, ok := self.connections[connectionKey]
			self.mutex.Unlock()
			if ok {
				result := pending.Result{Data: frame.Result}
				if frame.Error != nil {
					result.Error = *frame.Error
				}
				terminal.pending.Resolve(*frame.ID, result)
			}
			continue
		}
	}
}

func (self *Relay) pingLoop(connectionKey string, connection *websocket.Conn, done chan struct{}) {
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

func (self *Relay) onDisconnect(connectionKey string, connection *websocket.Conn) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	terminal, ok := self.connections[connectionKey]
	if !ok || terminal.connection != connection {
		return
	}

	terminal.pending.RejectAll("terminal client disconnected")
	delete(self.connections, connectionKey)

	log.Infof("terminal: client disconnected user=%s id=%s", terminal.userId, terminal.id)
}
