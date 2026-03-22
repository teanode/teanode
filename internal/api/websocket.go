package api

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

// MessageTransport abstracts message-level I/O so that both WebSocket
// connections and mux-stream proxy connections share the same RPC dispatch.
type MessageTransport interface {
	// ReadMessage returns the message type (1=text, 2=binary) and payload.
	ReadMessage() (messageType int, payload []byte, err error)
	// WriteTextMessage sends a text message.
	WriteTextMessage(data []byte) error
	// WriteBinaryMessage sends a binary message.
	WriteBinaryMessage(data []byte) error
	// Close closes the transport.
	Close() error
}

// webSocketTransport wraps a gorilla/websocket.Conn as a MessageTransport.
type webSocketTransport struct {
	connection *websocket.Conn
	writeMutex sync.Mutex
}

func (self *webSocketTransport) ReadMessage() (int, []byte, error) {
	return self.connection.ReadMessage()
}

func (self *webSocketTransport) WriteTextMessage(data []byte) error {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	return self.connection.WriteMessage(websocket.TextMessage, data)
}

func (self *webSocketTransport) WriteBinaryMessage(data []byte) error {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	return self.connection.WriteMessage(websocket.BinaryMessage, data)
}

func (self *webSocketTransport) Close() error {
	return self.connection.Close()
}

func loadPublicUrlFromStore(ctx context.Context) string {
	publicUrl := ""
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, getError := transaction.GetConfiguration(ctx, nil)
		if getError != nil || configuration == nil || configuration.Node == nil || configuration.Node.PublicURL == nil {
			return nil
		}
		publicUrl = *configuration.Node.PublicURL
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

func (self *api) isWebSocketOriginAllowed(request *http.Request) bool {
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

func (self *api) handleWebSocket(writer http.ResponseWriter, request *http.Request) error {
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
	transport := &webSocketTransport{connection: connection}
	webSocketConnection := newWebSocketConnection(request.Context(), transport, self)
	webSocketConnection.serve()
	return nil
}

// HandleStreamConnection serves an RPC session over a MessageTransport.
// This is used by the cloud proxy to bridge a mux stream to the local API.
func (self *api) HandleStreamConnection(ctx context.Context, transport MessageTransport) {
	webSocketConnection := newWebSocketConnection(ctx, transport, self)
	webSocketConnection.serve()
}

// webSocketConnection manages a single WebSocket or stream connection.
// It implements pubsub.Subscriber to receive broadcast events.
type webSocketConnection struct {
	transport MessageTransport
	api       *api
	ctx       context.Context
	id        string // unique connection identifier (for tab broker cleanup)

	// Idempotency deduplication: method+id -> expiry time
	deduplication sync.Map // map[string]time.Time

	activeVoiceMutex   sync.RWMutex
	activeVoiceSession *voice.Session
}

func newWebSocketConnection(ctx context.Context, transport MessageTransport, api *api) *webSocketConnection {
	return &webSocketConnection{
		transport: transport,
		api:       api,
		ctx:       ctx,
		id:        security.NewULID(),
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
	defer func() { _ = self.transport.Close() }()
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
		messageType, rawMessage, err := self.transport.ReadMessage()
		if err != nil {
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
	result, err := self.handleRpc(frame)
	if err != nil {
		if handlerErr, ok := err.(*rpcHandlerError); ok {
			self.sendError(frame.ID, handlerErr.code, handlerErr.message)
		} else {
			self.sendError(frame.ID, 500, err.Error())
		}
		return
	}
	self.sendResponse(frame.ID, result)
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
	data, err := json.Marshal(value)
	if err != nil {
		log.Errorf("ws json marshal error: %v", err)
		return
	}
	if err := self.transport.WriteTextMessage(data); err != nil {
		log.Errorf("ws write error: %v", err)
	}
}

func (self *webSocketConnection) writeBinary(data []byte) {
	if err := self.transport.WriteBinaryMessage(data); err != nil {
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
