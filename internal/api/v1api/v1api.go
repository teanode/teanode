// Package v1api implements the versioned REST + WebSocket API for the gw.
package v1api

import (
	"sync"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/util/ratelimit"
	"github.com/teanode/teanode/internal/web"
)

var log = logging.MustGetLogger("v1api")

// v1Api is the v1 API component. It implements web.Component.
type v1Api struct {
	gateway gw.Gateway

	// Per-IP rate limiter for auth endpoints (login, setup).
	authBucketsMutex sync.Mutex
	authBuckets      map[string]*ratelimit.Bucket
}

// New creates a new v1 API wired to the given Gateway.
func New(gateway gw.Gateway) *v1Api {
	return &v1Api{
		gateway:     gateway,
		authBuckets: make(map[string]*ratelimit.Bucket),
	}
}

// AddRoutes registers all v1 API routes on the router. Implements web.Component.
func (self *v1Api) AddRoutes(router *mux.Router) error {
	sub := router.PathPrefix("/api/v1").Subrouter()

	sub.Handle("/health", web.HandlerFunc(self.handleHealth))

	// Auth endpoints (exempt from auth middleware).
	sub.Handle("/auth/status", web.HandlerFunc(self.handleAuthStatus))
	sub.Handle("/auth/setup", web.HandlerFunc(self.handleAuthSetup))
	sub.Handle("/auth/login", web.HandlerFunc(self.handleAuthLogin))
	sub.Handle("/auth/logout", web.HandlerFunc(self.handleAuthLogout))

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

	sub.Handle("/chat/completions", web.HandlerFunc(self.handleChatCompletions))
	return nil
}
