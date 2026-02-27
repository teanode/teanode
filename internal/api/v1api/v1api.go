// Package v1api implements the versioned REST + WebSocket API for the gw.
package v1api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/util/ratelimit"
	"github.com/teanode/teanode/internal/util/sessiontracker"
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
	coordinator    *coordinators.Coordinator
	pubsub         *pubsub.PubSub
	sessionTracker *sessiontracker.SessionTracker
	browserRelay   *relaybrowser.Relay
	terminalRelay  *terminals.Relay

	// Per-IP rate limiter for auth endpoints (login, setup).
	rateLimitBucketsMutex sync.Mutex
	rateLimitBuckets      map[string]*rateLimitBucketEntry

	// Pending TTS synthesis requests keyed by token.
	synthesisTokensMutex sync.Mutex
	synthesisTokens      map[string]synthesisToken
}

// New creates a new v1 API wired to the given coordinator and pubsub.
func New(coordinator *coordinators.Coordinator, events *pubsub.PubSub, sessions *sessiontracker.SessionTracker, browserRelay *relaybrowser.Relay, terminalRelay *terminals.Relay) *v1Api {
	return &v1Api{
		coordinator:      coordinator,
		pubsub:           events,
		sessionTracker:   sessions,
		browserRelay:     browserRelay,
		terminalRelay:    terminalRelay,
		rateLimitBuckets: make(map[string]*rateLimitBucketEntry),
		synthesisTokens:  make(map[string]synthesisToken),
	}
}

// AddRoutes registers all v1 API routes on the router. Implements web.Component.
func (self *v1Api) AddRoutes(router *mux.Router) error {
	subrouter := router.PathPrefix("/api/v1").Subrouter()

	subrouter.Handle("/health", web.HandlerFunc(self.handleHealth))

	// Auth endpoints (exempt from auth middleware).
	subrouter.Handle("/auth/status", web.HandlerFunc(self.handleAuthStatus))
	subrouter.Handle("/auth/setup", web.HandlerFunc(self.handleAuthSetup))
	subrouter.Handle("/auth/login", web.HandlerFunc(self.handleAuthLogin))
	subrouter.Handle("/auth/logout", web.HandlerFunc(self.handleAuthLogout))

	subrouter.Handle("/websocket", web.HandlerFunc(self.handleWebSocket))

	if self.browserRelay != nil {
		subrouter.Handle("/browser", web.HandlerFunc(self.handleBrowserWebSocket))
	}
	if self.terminalRelay != nil {
		subrouter.Handle("/terminal", web.HandlerFunc(self.handleTerminalWebSocket))
	}
	subrouter.Handle("/media/upload", web.HandlerFunc(self.handleMediaUpload))
	subrouter.Handle("/media/{id}", web.HandlerFunc(self.handleMedia))

	subrouter.Handle("/audio/transcribe", web.HandlerFunc(self.handleAudioTranscribe))
	subrouter.Handle("/audio/synthesize", web.HandlerFunc(self.handleAudioSynthesize))
	subrouter.Handle("/audio/stream", web.HandlerFunc(self.handleAudioStream))

	subrouter.Handle("/chat/completions", web.HandlerFunc(self.handleChatCompletions))
	return nil
}

func (self *v1Api) handleBrowserWebSocket(writer http.ResponseWriter, request *http.Request) error {
	return self.browserRelay.HandleWebSocket(writer, request)
}

func (self *v1Api) handleTerminalWebSocket(writer http.ResponseWriter, request *http.Request) error {
	return self.terminalRelay.HandleWebSocket(writer, request)
}
