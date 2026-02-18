package v1api

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/gw"
)

// webSocketConnection manages a single WebSocket connection.
// It implements gw.Subscriber to receive broadcast events from the gateway.
type webSocketConnection struct {
	connection *websocket.Conn
	api        *v1Api
	writeMutex sync.Mutex
	sessionId  string

	// Idempotency deduplication: method+id -> expiry time
	deduplication sync.Map // map[string]time.Time
}

func newWebSocketConnection(connection *websocket.Conn, api *v1Api, sessionId string) *webSocketConnection {
	return &webSocketConnection{
		connection: connection,
		api:        api,
		sessionId:  sessionId,
	}
}

// OnEvent implements gw.Subscriber. It forwards gateway events to this WebSocket client.
func (self *webSocketConnection) OnEvent(eventType gw.EventType, payload interface{}) {
	self.sendEvent(eventType, payload)
}

func (self *webSocketConnection) serve() {
	defer self.connection.Close()
	self.api.gateway.Subscribe(self)
	defer self.api.gateway.Unsubscribe(self)

	for {
		_, rawMessage, err := self.connection.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Errorf("ws read error: %v", err)
			}
			return
		}

		var frame requestFrame
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

func (self *webSocketConnection) dispatch(frame requestFrame) {
	switch frame.Method {
	case "connect":
		self.handleConnect(frame)
	case "health":
		self.handleHealth(frame)
	case "agents.list":
		self.handleAgentsList(frame)
	case "conversations.send":
		self.handleConversationsSend(frame)
	case "conversations.history":
		self.handleConversationsHistory(frame)
	case "conversations.abort":
		self.handleConversationsAbort(frame)
	case "conversations.list":
		self.handleConversationsList(frame)
	case "conversations.delete":
		self.handleConversationsDelete(frame)
	case "models.list":
		self.handleModelsList(frame)
	case "jobs.list":
		self.handleJobsList(frame)
	case "jobs.create":
		self.handleJobsCreate(frame)
	case "jobs.update":
		self.handleJobsUpdate(frame)
	case "jobs.delete":
		self.handleJobsDelete(frame)
	case "jobs.trigger":
		self.handleJobsTrigger(frame)
	case "config.schema":
		self.handleConfigSchema(frame)
	case "config.get":
		self.handleConfigGet(frame)
	case "config.update":
		self.handleConfigUpdate(frame)
	case "agents.config.schema":
		self.handleAgentsConfigSchema(frame)
	case "agents.config.list":
		self.handleAgentsConfigList(frame)
	case "agents.config.save":
		self.handleAgentsConfigSave(frame)
	case "agents.config.delete":
		self.handleAgentsConfigDelete(frame)
	case "agents.setActive":
		self.handleAgentsSetActive(frame)
	case "conversations.setActive":
		self.handleConversationsSetActive(frame)
	case "sessions.list":
		self.handleSessionsList(frame)
	case "sessions.revoke":
		self.handleSessionsRevoke(frame)
	case "auth.getToken":
		self.handleAuthGetToken(frame)
	case "auth.regenerateToken":
		self.handleAuthRegenerateToken(frame)
	case "auth.changePassword":
		self.handleAuthChangePassword(frame)
	default:
		self.sendError(frame.ID, 404, "unknown method: "+frame.Method)
	}
}

func (self *webSocketConnection) sendResponse(id string, payload interface{}) {
	self.writeJSON(responseFrame{
		Type:    "res",
		ID:      id,
		OK:      true,
		Payload: payload,
	})
}

func (self *webSocketConnection) sendError(id string, code int, message string) {
	self.writeJSON(responseFrame{
		Type:  "res",
		ID:    id,
		OK:    false,
		Error: &apiError{Code: code, Message: message},
	})
}

func (self *webSocketConnection) sendEvent(eventType gw.EventType, payload interface{}) {
	self.writeJSON(eventFrame{
		Type:    "event",
		Event:   string(eventType),
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
