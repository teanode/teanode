// Package v1api implements the versioned REST + WebSocket API for the gw.
package v1api

import (
	"github.com/gorilla/mux"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/web"
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

	sub.Handle("/health", web.HandlerFunc(self.handleHealth))
	sub.Handle("/chat/completions", web.HandlerFunc(self.handleChatCompletions))
	sub.HandleFunc("/websocket", self.handleWebSocket)

	if self.gateway.BrowserRelay() != nil {
		sub.HandleFunc("/browser", self.gateway.BrowserRelay().HandleWebSocket)
	}
	if self.gateway.TerminalRelay() != nil {
		sub.HandleFunc("/terminal", self.gateway.TerminalRelay().HandleWebSocket)
	}
	if self.gateway.MediaStore() != nil {
		sub.Handle("/media/{id}", web.HandlerFunc(self.handleMedia))
	}

	return nil
}
