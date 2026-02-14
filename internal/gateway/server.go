package gateway

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/agent"
	"github.com/teanode/teanode/internal/browser"
	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/cron"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/provider"
	tterminal "github.com/teanode/teanode/internal/terminal"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/deferutil"
)

//go:embed static
var staticFiles embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(request *http.Request) bool { return true },
}

// Server is the main HTTP + WebSocket gateway.
type Server struct {
	Config        *config.Config
	AgentRegistry *agent.AgentRegistry
	BrowserRelay  *browser.Relay
	TerminalRelay *tterminal.Relay
	Scheduler     *cron.Scheduler
	MediaStore    *media.Store

	clientsMutex    sync.RWMutex
	clients         map[*webSocketConnection]struct{}
	activeRunsMutex sync.RWMutex
	activeRuns      map[string]string // sessionKey → runId

	modelsMutex sync.RWMutex
	models      map[string][]provider.ModelInfo // provider name → models
	modelsTime  time.Time                       // when cache was populated
}

// resolveRunner returns the runner for the given agent ID, defaulting to "main".
func (self *Server) resolveRunner(agentId string) *agent.Runner {
	if agentId == "" {
		agentId = config.DefaultAgentID
	}
	runner := self.AgentRegistry.Get(agentId)
	if runner == nil {
		runner = self.AgentRegistry.Default()
	}
	return runner
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
	if self.MediaStore != nil {
		router.HandleFunc("/media/", self.handleMedia)
	}

	// Serve embedded static files with SPA history fallback.
	staticSub, _ := fs.Sub(staticFiles, "static")
	router.Handle("/", spaHandler(http.FS(staticSub)))

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

// spaHandler serves static files from the given filesystem, falling back to
// index.html for any path that doesn't match a real file. This supports
// client-side (history API) routing.
func spaHandler(fileSystem http.FileSystem) http.Handler {
	fileServer := http.FileServer(fileSystem)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		// Try opening the requested file.
		file, err := fileSystem.Open(path)
		if err != nil {
			// File doesn't exist — serve index.html for SPA routing.
			request.URL.Path = "/"
			fileServer.ServeHTTP(writer, request)
			return
		}
		file.Close()
		fileServer.ServeHTTP(writer, request)
	})
}

func (self *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(`{"status":"ok"}`))
}

func (self *Server) handleMedia(writer http.ResponseWriter, request *http.Request) {
	// Extract media ID from path: /media/{id}
	mediaId := strings.TrimPrefix(request.URL.Path, "/media/")
	if mediaId == "" {
		http.Error(writer, "missing media id", http.StatusBadRequest)
		return
	}
	data, format, err := self.MediaStore.Load(mediaId)
	if err != nil {
		http.Error(writer, "not found", http.StatusNotFound)
		return
	}
	writer.Header().Set("Content-Type", media.MimeType(format))
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Write(data)
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

// modelsCache is the JSON structure written to ~/.teanode/models.json.
type modelsCache struct {
	FetchedAt time.Time                       `json:"fetchedAt"`
	Providers map[string][]provider.ModelInfo `json:"providers"`
}

const modelsCacheMaxAge = 24 * time.Hour

// loadModels returns the cached models or fetches from each provider's API.
func (self *Server) loadModels(ctx context.Context) (map[string][]provider.ModelInfo, error) {
	self.modelsMutex.RLock()
	if self.models != nil && time.Since(self.modelsTime) < modelsCacheMaxAge {
		result := self.models
		self.modelsMutex.RUnlock()
		return result, nil
	}
	self.modelsMutex.RUnlock()

	self.modelsMutex.Lock()
	defer self.modelsMutex.Unlock()

	// Double-check after acquiring write lock.
	if self.models != nil && time.Since(self.modelsTime) < modelsCacheMaxAge {
		return self.models, nil
	}

	// Try loading from disk cache.
	modelsFile, err := config.ModelsFile()
	if err == nil {
		if data, err := os.ReadFile(modelsFile); err == nil {
			var cache modelsCache
			if err := json.Unmarshal(data, &cache); err == nil && time.Since(cache.FetchedAt) < modelsCacheMaxAge {
				self.models = cache.Providers
				self.modelsTime = cache.FetchedAt
				self.updateRunnerContextWindows(cache.Providers)
				return cache.Providers, nil
			}
		}
	}

	// Fetch from each provider's API. Use the default runner to get providers.
	defaultRunner := self.AgentRegistry.Default()
	if defaultRunner == nil {
		return nil, fmt.Errorf("no default agent runner")
	}
	_, providers, _, _, _ := defaultRunner.Snapshot()
	result := make(map[string][]provider.ModelInfo)
	for _, name := range providers.ProviderNames() {
		client, _, err := providers.Resolve(provider.QualifyModel(name, "dummy"))
		if err != nil {
			continue
		}
		models, err := client.ListModels(ctx)
		if err != nil {
			log.Debugf("failed to fetch models from %s: %v", name, err)
			continue
		}
		result[name] = models
	}

	self.models = result
	self.modelsTime = time.Now()

	// Write to disk cache.
	if modelsFile != "" {
		cache := modelsCache{FetchedAt: self.modelsTime, Providers: result}
		if data, err := json.MarshalIndent(cache, "", "  "); err == nil {
			if err := atomicfile.WriteFile(modelsFile, data); err != nil {
				log.Debugf("failed to write models cache: %v", err)
			}
		}
	}

	self.updateRunnerContextWindows(result)
	return result, nil
}

// InvalidateModelsCache clears the in-memory models cache so the next
// loadModels call will re-fetch from disk or API.
func (self *Server) InvalidateModelsCache() {
	self.modelsMutex.Lock()
	self.models = nil
	self.modelsTime = time.Time{}
	self.modelsMutex.Unlock()
}

func (self *Server) updateRunnerContextWindows(models map[string][]provider.ModelInfo) {
	self.AgentRegistry.ForEach(func(agentId string, runner *agent.Runner) {
		for providerName, modelList := range models {
			runner.SetModels(providerName, modelList)
		}
	})
}
