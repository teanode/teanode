// Package v1api implements the versioned REST + WebSocket API for the gw.
package v1api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/util/ratelimit"
	"github.com/teanode/teanode/internal/web"
)

var log = logging.MustGetLogger("v1api")

// synthesisToken holds the parameters for a pending TTS synthesis request.
// Tokens are single-use and expire after 60 seconds.
type synthesisToken struct {
	Text      string
	Voice     string
	Speed     float64
	ExpiresAt time.Time
}

type rateLimitBucketEntry struct {
	bucket   *ratelimit.Bucket
	lastSeen time.Time
}

// v1Api is the v1 API component. It implements web.Component.
type v1Api struct {
	gateway gw.Gateway

	// Per-IP rate limiter for auth endpoints (login, setup).
	rateLimitBucketsMutex sync.Mutex
	rateLimitBuckets      map[string]*rateLimitBucketEntry

	// Pending TTS synthesis requests keyed by token.
	synthesisTokensMutex sync.Mutex
	synthesisTokens      map[string]synthesisToken
}

// New creates a new v1 API wired to the given Gateway.
func New(gateway gw.Gateway) *v1Api {
	return &v1Api{
		gateway:          gateway,
		rateLimitBuckets: make(map[string]*rateLimitBucketEntry),
		synthesisTokens:  make(map[string]synthesisToken),
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

	sub.Handle("/websocket", web.HandlerFunc(self.handleWebSocket))

	if self.gateway.BrowserRelay() != nil {
		sub.Handle("/browser", web.HandlerFunc(self.handleBrowserWebSocket))
	}
	if self.gateway.TerminalRelay() != nil {
		sub.Handle("/terminal", web.HandlerFunc(self.handleTerminalWebSocket))
	}
	sub.Handle("/media/upload", web.HandlerFunc(self.handleMediaUpload))
	sub.Handle("/media/{id}", web.HandlerFunc(self.handleMedia))

	sub.Handle("/audio/transcribe", web.HandlerFunc(self.handleAudioTranscribe))
	sub.Handle("/audio/synthesize", web.HandlerFunc(self.handleAudioSynthesize))
	sub.Handle("/audio/stream", web.HandlerFunc(self.handleAudioStream))

	sub.Handle("/chat/completions", web.HandlerFunc(self.handleChatCompletions))
	return nil
}

func (self *v1Api) handleBrowserWebSocket(writer http.ResponseWriter, request *http.Request) error {
	return self.gateway.BrowserRelay().HandleWebSocket(writer, request)
}

func (self *v1Api) handleTerminalWebSocket(writer http.ResponseWriter, request *http.Request) error {
	return self.gateway.TerminalRelay().HandleWebSocket(writer, request)
}
