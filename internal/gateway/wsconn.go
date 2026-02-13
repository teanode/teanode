package gateway

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ziyan/teanode/internal/types"
)

// webSocketConnection manages a single WebSocket connection.
type webSocketConnection struct {
	connection    *websocket.Conn
	server        *Server
	writeMutex    sync.Mutex

	// Active agent runs keyed by run ID.
	runs          sync.Map // map[string]context.CancelFunc

	// Idempotency deduplication: method+id -> expiry time
	deduplication sync.Map // map[string]time.Time
}

func newWebSocketConnection(connection *websocket.Conn, server *Server) *webSocketConnection {
	return &webSocketConnection{
		connection: connection,
		server:     server,
	}
}

func (self *webSocketConnection) serve() {
	defer self.connection.Close()
	self.server.registerClient(self)
	defer self.server.unregisterClient(self)

	for {
		_, rawMessage, err := self.connection.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Errorf("ws read error: %v", err)
			}
			return
		}

		var frame types.RequestFrame
		if err := json.Unmarshal(rawMessage, &frame); err != nil {
			self.sendError(frame.ID, 400, "invalid frame")
			continue
		}

		if frame.Type != "req" {
			continue
		}

		// Idempotency check.
		dedupKey := frame.Method + ":" + frame.ID
		if expiry, loaded := self.deduplication.Load(dedupKey); loaded {
			if time.Now().Before(expiry.(time.Time)) {
				continue // duplicate, skip
			}
		}
		self.deduplication.Store(dedupKey, time.Now().Add(15*time.Minute))

		self.dispatch(frame)
	}
}

func (self *webSocketConnection) dispatch(frame types.RequestFrame) {
	switch frame.Method {
	case "connect":
		self.handleConnect(frame)
	case "health":
		self.handleHealth(frame)
	case "chat.send":
		self.handleChatSend(frame)
	case "chat.history":
		self.handleChatHistory(frame)
	case "chat.abort":
		self.handleChatAbort(frame)
	case "sessions.list":
		self.handleSessionsList(frame)
	case "sessions.rename":
		self.handleSessionsRename(frame)
	case "sessions.delete":
		self.handleSessionsDelete(frame)
	case "crons.list":
		self.handleCronsList(frame)
	case "crons.create":
		self.handleCronsCreate(frame)
	case "crons.update":
		self.handleCronsUpdate(frame)
	case "crons.delete":
		self.handleCronsDelete(frame)
	case "crons.trigger":
		self.handleCronsTrigger(frame)
	default:
		self.sendError(frame.ID, 404, "unknown method: "+frame.Method)
	}
}

func (self *webSocketConnection) sendResponse(id string, payload interface{}) {
	self.writeJSON(types.ResponseFrame{
		Type:    "res",
		ID:      id,
		OK:      true,
		Payload: payload,
	})
}

func (self *webSocketConnection) sendError(id string, code int, message string) {
	self.writeJSON(types.ResponseFrame{
		Type:  "res",
		ID:    id,
		OK:    false,
		Error: &types.RPCError{Code: code, Message: message},
	})
}

func (self *webSocketConnection) sendEvent(event string, payload interface{}) {
	self.writeJSON(types.EventFrame{
		Type:    "event",
		Event:   event,
		Payload: payload,
	})
}

func (self *webSocketConnection) writeJSON(value interface{}) {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	if err := self.connection.WriteJSON(value); err != nil {
		log.Errorf("ws write error: %v", err)
	}
}
