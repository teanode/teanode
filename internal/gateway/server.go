package gateway

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/config"
	tterminal "github.com/ziyan/teanode/internal/terminal"
	"github.com/ziyan/teanode/internal/cron"
	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/session"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.Get("gateway")

//go:embed static
var staticFiles embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

// Server is the main HTTP + WebSocket gateway.
type Server struct {
	Config        *config.Config
	Agent         *agent.Runner
	Sessions      *session.Store
	BrowserRelay  *browser.Relay
	TerminalRelay *tterminal.Relay
	Scheduler     *cron.Scheduler

	clientsMutex    sync.RWMutex
	clients         map[*webSocketConnection]struct{}
	activeRunsMutex sync.RWMutex
	activeRuns      map[string]string // sessionKey → runId
}

// Start starts the HTTP server and blocks until ctx is cancelled.
func (self *Server) Start(ctx context.Context) error {
	self.clients = make(map[*webSocketConnection]struct{})
	self.activeRuns = make(map[string]string)

	router := http.NewServeMux()
	router.HandleFunc("/health", self.handleHealth)
	router.HandleFunc("/v1/chat/completions", self.handleChatCompletions)
	router.HandleFunc("/ws", self.handleWebSocket)
	if self.BrowserRelay != nil {
		router.HandleFunc("/browser", self.BrowserRelay.HandleWebSocket)
	}
	if self.TerminalRelay != nil {
		router.HandleFunc("/terminal", self.TerminalRelay.HandleWebSocket)
	}

	// Serve embedded static files at /
	staticSub, _ := fs.Sub(staticFiles, "static")
	router.Handle("/", http.FileServer(http.FS(staticSub)))

	if self.Scheduler != nil {
		if err := self.Scheduler.Start(); err != nil {
			return err
		}
	}

	address := self.listenAddr()
	server := &http.Server{
		Addr:    address,
		Handler: self.withAuth(router),
	}

	go func() {
		defer deferutil.Recover()
		<-ctx.Done()
		if self.Scheduler != nil {
			self.Scheduler.Stop()
		}
		server.Close()
	}()

	log.Infof("TeaNode gateway listening on %s", address)
	err := server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (self *Server) listenAddr() string {
	host := "127.0.0.1"
	if self.Config.Gateway.Bind == "lan" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", self.Config.Gateway.Port))
}

func (self *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{"status":"ok"}`))
}

func (self *Server) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	connection, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		log.Errorf("websocket upgrade error: %v", err)
		return
	}
	webSocketConnection := newWebSocketConnection(connection, self)
	webSocketConnection.serve()
}

// withAuth wraps a handler with optional token/password auth.
func (self *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if self.Config.Gateway.Auth == nil {
			next.ServeHTTP(writer, request)
			return
		}

		// Skip auth for health, browser extension, and terminal endpoints
		if request.URL.Path == "/health" || request.URL.Path == "/browser" || request.URL.Path == "/terminal" {
			next.ServeHTTP(writer, request)
			return
		}

		token := self.Config.Gateway.Auth.Token
		if token != "" {
			auth := request.Header.Get("Authorization")
			if auth == "Bearer "+token {
				next.ServeHTTP(writer, request)
				return
			}
			// Also check query param for WebSocket connections
			if request.URL.Query().Get("token") == token {
				next.ServeHTTP(writer, request)
				return
			}
		}

		password := self.Config.Gateway.Auth.Password
		if password != "" {
			_, pass, ok := request.BasicAuth()
			if ok && pass == password {
				next.ServeHTTP(writer, request)
				return
			}
		}

		http.Error(writer, "unauthorized", http.StatusUnauthorized)
	})
}

func (self *Server) registerClient(client *webSocketConnection) {
	self.clientsMutex.Lock()
	self.clients[client] = struct{}{}
	self.clientsMutex.Unlock()
}

func (self *Server) unregisterClient(client *webSocketConnection) {
	self.clientsMutex.Lock()
	delete(self.clients, client)
	self.clientsMutex.Unlock()
}

// Broadcast sends an event to all connected WebSocket clients.
func (self *Server) Broadcast(event string, payload interface{}) {
	self.clientsMutex.RLock()
	defer self.clientsMutex.RUnlock()
	for client := range self.clients {
		client.sendEvent(event, payload)
	}
}

// SetActiveRun records that a run is active for a session.
func (self *Server) SetActiveRun(sessionKey, runId string) {
	self.activeRunsMutex.Lock()
	self.activeRuns[sessionKey] = runId
	self.activeRunsMutex.Unlock()
}

// ClearActiveRun removes the active run for a session if it matches the given runId.
func (self *Server) ClearActiveRun(sessionKey, runId string) {
	self.activeRunsMutex.Lock()
	if self.activeRuns[sessionKey] == runId {
		delete(self.activeRuns, sessionKey)
	}
	self.activeRunsMutex.Unlock()
}

// GetActiveRun returns the active run ID for a session, or empty string if none.
func (self *Server) GetActiveRun(sessionKey string) string {
	self.activeRunsMutex.RLock()
	defer self.activeRunsMutex.RUnlock()
	return self.activeRuns[sessionKey]
}
