// Package v1api implements the versioned REST + WebSocket API for the gw.
package v1api

import (
	"sync"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/gw"
)

var log = logging.MustGetLogger("v1api")

// API is the v1 API component. It implements web.Component and manages
// WebSocket client connections for broadcasting events.
type API struct {
	gateway gw.Gateway

	clientsMutex sync.RWMutex
	clients      map[*webSocketConnection]struct{}
}

// New creates a new v1 API wired to the given Gateway.
func New(gateway gw.Gateway) *API {
	return &API{
		gateway: gateway,
		clients: make(map[*webSocketConnection]struct{}),
	}
}

// AddRoutes registers all v1 API routes on the router. Implements web.Component.
func (self *API) AddRoutes(router *mux.Router) error {
	sub := router.PathPrefix("/api/v1").Subrouter()

	sub.HandleFunc("/health", self.handleHealth)
	sub.HandleFunc("/chat/completions", self.handleChatCompletions)
	sub.HandleFunc("/websocket", self.handleWebSocket)

	if self.gateway.BrowserRelay() != nil {
		sub.HandleFunc("/browser", self.gateway.BrowserRelay().HandleWebSocket)
	}
	if self.gateway.TerminalRelay() != nil {
		sub.HandleFunc("/terminal", self.gateway.TerminalRelay().HandleWebSocket)
	}
	if self.gateway.MediaStore() != nil {
		sub.HandleFunc("/media/{id}", self.handleMedia)
	}

	return nil
}

// Broadcast sends an event to all connected WebSocket clients.
func (self *API) Broadcast(event string, payload interface{}) {
	self.clientsMutex.RLock()
	defer self.clientsMutex.RUnlock()
	for client := range self.clients {
		client.sendEvent(event, payload)
	}
}

func (self *API) registerClient(client *webSocketConnection) {
	self.clientsMutex.Lock()
	self.clients[client] = struct{}{}
	self.clientsMutex.Unlock()
}

func (self *API) unregisterClient(client *webSocketConnection) {
	self.clientsMutex.Lock()
	delete(self.clients, client)
	self.clientsMutex.Unlock()
}
