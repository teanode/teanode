// Package v1api implements the versioned REST + WebSocket API for the gw.
package v1api

import (
	"github.com/gorilla/mux"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/gw"
)

var log = logging.MustGetLogger("v1api")

// API is the v1 API component. It implements web.Component.
type API struct {
	gateway gw.Gateway
}

// New creates a new v1 API wired to the given Gateway.
func New(gateway gw.Gateway) *API {
	return &API{
		gateway: gateway,
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
