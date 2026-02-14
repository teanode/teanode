package terminal

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

// terminalConnection holds the state for a single named terminal connection.
type terminalConnection struct {
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

// HandleWebSocket upgrades the HTTP connection and manages the terminal client link.
func (self *Relay) HandleWebSocket(writer http.ResponseWriter, request *http.Request) {
	id := request.URL.Query().Get("id")
	if id == "" {
		id = "default"
	}

	websocketConnection, err := wsUpgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("terminal: upgrade error: %v", err)
		return
	}

	self.mutex.Lock()
	// If a connection with this ID already exists, replace it.
	if existing, ok := self.connections[id]; ok {
		existing.connection.Close()
		existing.pending.RejectAll("connection replaced")
	}
	terminal := &terminalConnection{
		connection: websocketConnection,
		pending:    pending.NewRequests(),
		done:       make(chan struct{}),
	}
	self.connections[id] = terminal
	done := terminal.done
	self.mutex.Unlock()

	log.Infof("terminal: client connected id=%s", id)

	go self.pingLoop(id, websocketConnection, done)
	self.readLoop(id, websocketConnection, done)
}

// Connected reports whether at least one terminal client is connected.
func (self *Relay) Connected() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return len(self.connections) > 0
}

// Connections returns a snapshot of all connection IDs with machine info.
func (self *Relay) Connections() []ConnectionInfo {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	out := make([]ConnectionInfo, 0, len(self.connections))
	for id, tc := range self.connections {
		out = append(out, ConnectionInfo{ID: id, Machine: tc.machine})
	}
	return out
}

// DefaultConnection returns the ID of the first connected terminal,
// providing backwards compatibility when no connectionId is specified.
func (self *Relay) DefaultConnection() (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for id := range self.connections {
		return id, nil
	}
	return "", errors.New("terminal client not connected")
}

// SendCommand sends a command to a specific terminal client and waits for the result.
func (self *Relay) SendCommand(ctx context.Context, connectionId string, method string, parameters interface{}) (json.RawMessage, error) {
	self.mutex.Lock()
	terminal, ok := self.connections[connectionId]
	if !ok {
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

func (self *Relay) readLoop(id string, connection *websocket.Conn, done chan struct{}) {
	defer func() {
		self.onDisconnect(id, connection)
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
				if tc, ok := self.connections[id]; ok {
					tc.machine = info
				}
				self.mutex.Unlock()
				log.Infof("terminal: attach id=%s host=%s user=%s tz=%s", id, info.Hostname, info.Username, info.Timezone)
			}
			continue
		}

		if frame.ID != nil && (frame.Result != nil || frame.Error != nil) {
			self.mutex.Lock()
			terminal, ok := self.connections[id]
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

func (self *Relay) pingLoop(id string, connection *websocket.Conn, done chan struct{}) {
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

func (self *Relay) onDisconnect(id string, connection *websocket.Conn) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	terminal, ok := self.connections[id]
	if !ok || terminal.connection != connection {
		return
	}

	terminal.pending.RejectAll("terminal client disconnected")
	delete(self.connections, id)

	log.Infof("terminal: client disconnected id=%s", id)
}
