package v1api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/voice"
)

func loadPublicUrlFromStore(ctx context.Context) string {
	publicUrl := ""
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, getError := transaction.GetConfiguration(ctx, nil)
		if getError != nil || configuration == nil || configuration.Gateway == nil || configuration.Gateway.PublicURL == nil {
			return nil
		}
		publicUrl = *configuration.Gateway.PublicURL
		return nil
	})
	return publicUrl
}

func splitHostPortDefault(rawHost string, tls bool) (string, string) {
	host, port, err := net.SplitHostPort(rawHost)
	if err == nil {
		return strings.ToLower(host), port
	}
	defaultPort := "80"
	if tls {
		defaultPort = "443"
	}
	return strings.ToLower(rawHost), defaultPort
}

func sameOriginHost(leftHost string, leftTls bool, rightHost string, rightTls bool) bool {
	leftName, leftPort := splitHostPortDefault(leftHost, leftTls)
	rightName, rightPort := splitHostPortDefault(rightHost, rightTls)
	return leftName == rightName && leftPort == rightPort
}

func (self *v1Api) isWebSocketOriginAllowed(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	originUrl, err := url.Parse(origin)
	if err != nil || originUrl.Host == "" {
		return false
	}
	if strings.EqualFold(originUrl.Scheme, "chrome-extension") {
		return true
	}
	originTls := strings.EqualFold(originUrl.Scheme, "https")
	requestTls := request.TLS != nil
	if sameOriginHost(originUrl.Host, originTls, request.Host, requestTls) {
		return true
	}

	publicUrl := loadPublicUrlFromStore(request.Context())
	if publicUrl == "" {
		return false
	}
	parsedPublicUrl, err := url.Parse(publicUrl)
	if err != nil || parsedPublicUrl.Host == "" {
		return false
	}
	publicTls := strings.EqualFold(parsedPublicUrl.Scheme, "https")
	return sameOriginHost(originUrl.Host, originTls, parsedPublicUrl.Host, publicTls)
}

func (self *v1Api) handleWebSocket(writer http.ResponseWriter, request *http.Request) error {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(request *http.Request) bool {
			return self.isWebSocketOriginAllowed(request)
		},
	}
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("websocket upgrade error: %v", err)
		return err
	}
	webSocketConnection := newWebSocketConnection(connection, self, request.Context())
	webSocketConnection.serve()
	return nil
}

// webSocketConnection manages a single WebSocket connection.
// It implements pubsub.Subscriber to receive broadcast events.
type webSocketConnection struct {
	connection *websocket.Conn
	api        *v1Api
	ctx        context.Context
	writeMutex sync.Mutex
	id         string // unique connection identifier (for tab broker cleanup)

	// Idempotency deduplication: method+id -> expiry time
	deduplication sync.Map // map[string]time.Time

	activeVoiceMutex   sync.RWMutex
	activeVoiceSession *voice.Session
}

func newWebSocketConnection(connection *websocket.Conn, api *v1Api, ctx context.Context) *webSocketConnection {
	return &webSocketConnection{
		connection: connection,
		api:        api,
		ctx:        ctx,
		id:         security.NewULID(),
	}
}

// connectionId returns the unique identifier for this connection.
func (self *webSocketConnection) connectionId() string {
	return self.id
}

// OnEvent implements pubsub.Subscriber. It forwards events to this WebSocket client.
func (self *webSocketConnection) OnEvent(eventType pubsub.EventType, payload interface{}) {
	if !self.shouldDeliverEvent(payload) {
		return
	}
	self.sendEvent(eventType, payload)
}

func (self *webSocketConnection) shouldDeliverEvent(payload interface{}) bool {
	message, ok := payload.(map[string]interface{})
	if !ok {
		return true
	}
	rawUserId, hasUserId := message["userId"]
	if !hasUserId {
		return true
	}
	eventUserId, ok := rawUserId.(string)
	if !ok || eventUserId == "" {
		return true
	}
	return eventUserId == self.userId()
}

func (self *webSocketConnection) serve() {
	defer self.connection.Close()
	sessionId := self.sessionId()

	self.api.sessionTracker.MarkConnected(sessionId)
	defer self.api.sessionTracker.MarkDisconnected(sessionId)

	self.api.pubsub.Subscribe(self)
	defer self.api.pubsub.Unsubscribe(self)

	defer func() {
		if session := self.getActiveVoiceSession(); session != nil {
			session.Close()
			self.clearActiveVoiceSession(session)
		}
	}()

	// Clean up tab attachments owned by this connection.
	defer func() {
		if broker := self.api.coordinator.TabBroker(); broker != nil {
			broker.DetachAllForConnection(self.connectionId())
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
			session := self.getActiveVoiceSession()
			if session == nil {
				log.Warningf("ws binary frame dropped: no active voice session, bytes=%d", len(rawMessage))
				continue
			}
			if err := session.HandleInputBinaryFrame(rawMessage); err != nil {
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
		deduplicationKey := frame.Method + ":" + frame.ID
		if expiry, loaded := self.deduplication.Load(deduplicationKey); loaded {
			if time.Now().Before(expiry.(time.Time)) {
				continue // duplicate, skip
			}
		}
		self.deduplication.Store(deduplicationKey, time.Now().Add(15*time.Minute))

		self.dispatch(frame)
	}
}

func (self *webSocketConnection) userId() string {
	if user := models.UserFromContext(self.ctx); user != nil {
		return user.ID
	}
	return ""
}

func (self *webSocketConnection) defaultAgentId() string {
	if user := models.UserFromContext(self.ctx); user != nil {
		return user.GetDefaultAgentID()
	}
	return ""
}

func (self *webSocketConnection) sessionId() string {
	if session := models.SessionFromContext(self.ctx); session != nil {
		return session.ID
	}
	return ""
}

func (self *webSocketConnection) isAdmin() bool {
	user := models.UserFromContext(self.ctx)
	if user == nil || user.Admin == nil {
		return false
	}
	return *user.Admin
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
	case "skills.local.list":
		self.handleSkillsLocalList(frame)
	case "skills.library.search":
		self.handleSkillsLibrarySearch(frame)
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
	case "secrets.list":
		self.handleSecretsList(frame)
	case "secrets.set":
		self.handleSecretsSet(frame)
	case "voice.providers":
		self.handleVoiceProviders(frame)
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
	case "conversations.todos.list":
		self.handleConversationsTodosList(frame)
	case "conversations.todos.batch":
		self.handleConversationsTodosBatch(frame)
	case "questions.list":
		self.handleQuestionsList(frame)
	case "questions.answer":
		self.handleQuestionsAnswer(frame)
	case "tab.attach":
		self.handleTabAttach(frame)
	case "tab.detach":
		self.handleTabDetach(frame)
	case "tab.commandResult":
		self.handleTabCommandResult(frame)
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

func (self *webSocketConnection) sendEvent(eventType pubsub.EventType, payload interface{}) {
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
	self.activeVoiceMutex.RLock()
	defer self.activeVoiceMutex.RUnlock()
	return self.activeVoiceSession
}

func (self *webSocketConnection) setActiveVoiceSession(session *voice.Session) bool {
	self.activeVoiceMutex.Lock()
	defer self.activeVoiceMutex.Unlock()
	if self.activeVoiceSession != nil {
		return false
	}
	self.activeVoiceSession = session
	return true
}

func (self *webSocketConnection) clearActiveVoiceSession(session *voice.Session) {
	self.activeVoiceMutex.Lock()
	defer self.activeVoiceMutex.Unlock()
	if self.activeVoiceSession == session {
		self.activeVoiceSession = nil
	}
}
