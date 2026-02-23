package v1api

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/voice"
)

// webSocketConnection manages a single WebSocket connection.
// It implements gw.Subscriber to receive broadcast events from the gateway.
type webSocketConnection struct {
	connection *websocket.Conn
	api        *v1Api
	writeMutex sync.Mutex
	sessionId  string
	userId     string

	// Idempotency deduplication: method+id -> expiry time
	deduplication sync.Map // map[string]time.Time

	activeVoiceMu      sync.RWMutex
	activeVoiceSession *voice.Session
}

func newWebSocketConnection(connection *websocket.Conn, api *v1Api, sessionId, userId string) *webSocketConnection {
	return &webSocketConnection{
		connection: connection,
		api:        api,
		sessionId:  sessionId,
		userId:     userId,
	}
}

// OnEvent implements gw.Subscriber. It forwards gateway events to this WebSocket client.
func (self *webSocketConnection) OnEvent(eventType gw.EventType, payload interface{}) {
	self.sendEvent(eventType, payload)
}

func (self *webSocketConnection) serve() {
	defer self.connection.Close()
	self.api.gateway.MarkSessionConnected(self.sessionId)
	defer self.api.gateway.MarkSessionDisconnected(self.sessionId)
	self.api.gateway.Subscribe(self)
	defer self.api.gateway.Unsubscribe(self)
	defer func() {
		if sess := self.getActiveVoiceSession(); sess != nil {
			sess.Close()
			self.clearActiveVoiceSession(sess)
		}
	}()

	for {
		messageType, rawMessage, err := self.connection.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Errorf("ws read error: %v", err)
			}
			return
		}

		if messageType == websocket.BinaryMessage {
			sess := self.getActiveVoiceSession()
			if sess == nil {
				log.Warningf("ws binary frame dropped: no active voice session, bytes=%d", len(rawMessage))
				continue
			}
			if err := sess.HandleInputBinaryFrame(rawMessage); err != nil {
				log.Warningf("invalid voice binary frame: %v", err)
			}
			continue
		}

		var frame requestFrame
		if err := json.Unmarshal(rawMessage, &frame); err != nil {
			self.sendError("", 400, "invalid frame")
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
	case "agents.avatar.set":
		self.handleAgentsAvatarSet(frame)
	case "agents.avatar.remove":
		self.handleAgentsAvatarRemove(frame)
	case "agents.setDefault":
		self.handleAgentsSetDefault(frame)
	case "conversations.setDefault":
		self.handleConversationsSetDefault(frame)
	case "sessions.list":
		self.handleSessionsList(frame)
	case "sessions.revoke":
		self.handleSessionsRevoke(frame)
	case "auth.tokens.list":
		self.handleAuthTokensList(frame)
	case "auth.tokens.create":
		self.handleAuthTokensCreate(frame)
	case "auth.tokens.delete":
		self.handleAuthTokensDelete(frame)
	case "auth.changePassword":
		self.handleAuthChangePassword(frame)
	case "users.list":
		self.handleUsersList(frame)
	case "users.create":
		self.handleUsersCreate(frame)
	case "users.delete":
		self.handleUsersDelete(frame)
	case "users.changePassword":
		self.handleUsersChangePassword(frame)
	case "users.update":
		self.handleUsersUpdate(frame)
	case "users.setRole":
		self.handleUsersSetRole(frame)
	case "profile.get":
		self.handleProfileGet(frame)
	case "profile.update":
		self.handleProfileUpdate(frame)
	case "profile.avatar.remove":
		self.handleProfileAvatarRemove(frame)
	case "skills.registry.list":
		self.handleSkillsRegistryList(frame)
	case "skills.registry.search":
		self.handleSkillsRegistrySearch(frame)
	case "skills.local.list":
		self.handleSkillsLocalList(frame)
	case "skills.install":
		self.handleSkillsInstall(frame)
	case "skills.installed.list":
		self.handleSkillsInstalledList(frame)
	case "skills.uninstall":
		self.handleSkillsUninstall(frame)
	case "skills.update":
		self.handleSkillsUpdate(frame)
	case "skills.setEnabled":
		self.handleSkillsSetEnabled(frame)
	case "voice.start":
		self.handleVoiceStart(frame)
	case "voice.end":
		self.handleVoiceEnd(frame)
	case "voice.response.cancel":
		self.handleVoiceResponseCancel(frame)
	case "voice.input.commit":
		self.handleVoiceInputCommit(frame)
	case "projects.list":
		self.handleProjectsList(frame)
	case "projects.create":
		self.handleProjectsCreate(frame)
	case "projects.rename":
		self.handleProjectsRename(frame)
	case "projects.delete":
		self.handleProjectsDelete(frame)
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

func (self *webSocketConnection) writeBinary(data []byte) {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	if err := self.connection.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Errorf("ws binary write error: %v", err)
	}
}

func (self *webSocketConnection) getActiveVoiceSession() *voice.Session {
	self.activeVoiceMu.RLock()
	defer self.activeVoiceMu.RUnlock()
	return self.activeVoiceSession
}

func (self *webSocketConnection) setActiveVoiceSession(session *voice.Session) bool {
	self.activeVoiceMu.Lock()
	defer self.activeVoiceMu.Unlock()
	if self.activeVoiceSession != nil {
		return false
	}
	self.activeVoiceSession = session
	return true
}

func (self *webSocketConnection) clearActiveVoiceSession(session *voice.Session) {
	self.activeVoiceMu.Lock()
	defer self.activeVoiceMu.Unlock()
	if self.activeVoiceSession == session {
		self.activeVoiceSession = nil
	}
}
