package gw

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/web"
	"gopkg.in/yaml.v3"
)

// gateway is the unexported concrete implementation of Gateway.
type gateway struct {
	config        *configs.Config
	agentRegistry *agents.AgentRegistry
	browserRelay  *browsers.Relay
	terminalRelay *terminals.Relay
	scheduler     *jobs.Scheduler
	summarizer    *agents.Summarizer
	mediaStore    *media.Store

	broadcast func(event string, payload interface{})

	activeRunsMutex sync.RWMutex
	activeRuns      map[string]string // conversationId → runId

	modelsMutex sync.RWMutex
	models      map[string][]provider.ModelInfo // provider name → models
	modelsTime  time.Time                       // when cache was populated
}

// --- Configuration access ---

func (self *gateway) Config() *configs.Config                 { return self.config }
func (self *gateway) SetConfig(configuration *configs.Config) { self.config = configuration }

// --- Subsystem access ---

func (self *gateway) AgentRegistry() *agents.AgentRegistry { return self.agentRegistry }
func (self *gateway) Scheduler() *jobs.Scheduler          { return self.scheduler }
func (self *gateway) Summarizer() *agents.Summarizer       { return self.summarizer }
func (self *gateway) MediaStore() *media.Store            { return self.mediaStore }
func (self *gateway) BrowserRelay() *browsers.Relay       { return self.browserRelay }
func (self *gateway) TerminalRelay() *terminals.Relay     { return self.terminalRelay }

// --- Domain operations ---

// ResolveRunner returns the runner for the given agent ID, defaulting to the configured default agents.
func (self *gateway) ResolveRunner(agentId string) *agents.Runner {
	if agentId == "" {
		agentId = self.agentRegistry.DefaultID()
	}
	runner := self.agentRegistry.Get(agentId)
	if runner == nil {
		runner = self.agentRegistry.Default()
	}
	return runner
}

// modelsCache is the YAML structure written to ~/.teanode/models.yaml.
type modelsCache struct {
	FetchedAt time.Time                       `json:"fetchedAt" yaml:"fetchedAt"`
	Providers map[string][]provider.ModelInfo `json:"providers" yaml:"providers"`
}

const modelsCacheMaxAge = 24 * time.Hour

// LoadModels returns the cached models or fetches from each provider's API.
func (self *gateway) LoadModels(ctx context.Context) (map[string][]provider.ModelInfo, error) {
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
	modelsFile, err := configs.ModelsFile()
	if err == nil {
		if data, err := os.ReadFile(modelsFile); err == nil {
			var cache modelsCache
			if err := yaml.Unmarshal(data, &cache); err == nil && time.Since(cache.FetchedAt) < modelsCacheMaxAge {
				self.models = cache.Providers
				self.modelsTime = cache.FetchedAt
				self.updateRunnerContextWindows(cache.Providers)
				return cache.Providers, nil
			}
		}
	}

	// Fetch from each provider's API. Use the default runner to get providers.
	defaultRunner := self.agentRegistry.Default()
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
		if data, err := yaml.Marshal(cache); err == nil {
			if err := atomicfile.WriteFile(modelsFile, data); err != nil {
				log.Debugf("failed to write models cache: %v", err)
			}
		}
	}

	self.updateRunnerContextWindows(result)
	return result, nil
}

// InvalidateModelsCache clears the in-memory models cache so the next
// LoadModels call will re-fetch from disk or API.
func (self *gateway) InvalidateModelsCache() {
	self.modelsMutex.Lock()
	self.models = nil
	self.modelsTime = time.Time{}
	self.modelsMutex.Unlock()
}

func (self *gateway) updateRunnerContextWindows(models map[string][]provider.ModelInfo) {
	self.agentRegistry.ForEach(func(agentId string, runner *agents.Runner) {
		for providerName, modelList := range models {
			runner.SetModels(providerName, modelList)
		}
	})
}

// --- Active agent / conversation ---

func (self *gateway) ActiveAgentID() string              { return self.agentRegistry.ActiveAgentID() }
func (self *gateway) ActiveConversationID(agentId string) string { return self.agentRegistry.ActiveConversationID(agentId) }

func (self *gateway) SetBroadcast(broadcast func(event string, payload interface{})) {
	self.broadcast = broadcast
}

func (self *gateway) SetActiveAgent(agentId string) error {
	err := self.agentRegistry.SetActiveAgent(agentId)
	if err == nil && self.broadcast != nil {
		self.broadcast("activeAgent", map[string]interface{}{
			"activeAgentId":        agentId,
			"activeConversationId": self.agentRegistry.ActiveConversationID(agentId),
		})
	}
	return err
}

func (self *gateway) SetActiveConversation(agentId, conversationId string) {
	self.agentRegistry.SetActiveConversation(agentId, conversationId)
	if self.broadcast != nil {
		self.broadcast("activeConversation", map[string]interface{}{
			"agentId":              agentId,
			"activeConversationId": conversationId,
		})
	}
}

func (self *gateway) SetActiveConversationIfUnset(agentId, conversationId string) bool {
	changed := self.agentRegistry.SetActiveConversationIfUnset(agentId, conversationId)
	if changed && self.broadcast != nil {
		self.broadcast("activeConversation", map[string]interface{}{
			"agentId":              agentId,
			"activeConversationId": conversationId,
		})
	}
	return changed
}

func (self *gateway) NewConversation(agentId string) string {
	conversationId := self.agentRegistry.NewConversation(agentId)
	if self.broadcast != nil {
		self.broadcast("activeConversation", map[string]interface{}{
			"agentId":              agentId,
			"activeConversationId": conversationId,
		})
	}
	return conversationId
}

// --- Run tracking ---

// SetActiveRun records that a run is active for a conversation.
func (self *gateway) SetActiveRun(conversationId, runId string) {
	self.activeRunsMutex.Lock()
	self.activeRuns[conversationId] = runId
	self.activeRunsMutex.Unlock()
}

// ClearActiveRun removes the active run for a conversation if it matches the given runId.
// Also notifies the summarizer so new/updated conversations get titled promptly.
func (self *gateway) ClearActiveRun(conversationId, runId string) {
	self.activeRunsMutex.Lock()
	if self.activeRuns[conversationId] == runId {
		delete(self.activeRuns, conversationId)
	}
	self.activeRunsMutex.Unlock()

	if self.summarizer != nil {
		self.summarizer.Notify()
	}
}

// GetActiveRun returns the active run ID for a conversation, or empty string if none.
func (self *gateway) GetActiveRun(conversationId string) string {
	self.activeRunsMutex.RLock()
	defer self.activeRunsMutex.RUnlock()
	return self.activeRuns[conversationId]
}

// --- Auth middleware ---

// AuthMiddleware returns a web.Middleware that enforces token/password auth.
func (self *gateway) AuthMiddleware() web.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if self.config.Gateway.Auth == nil {
				next.ServeHTTP(writer, request)
				return
			}

			// Skip auth for health, browser extension, and terminal endpoints.
			if request.URL.Path == "/api/v1/health" || request.URL.Path == "/api/v1/browser" || request.URL.Path == "/api/v1/terminal" {
				next.ServeHTTP(writer, request)
				return
			}

			token := self.config.Gateway.Auth.Token
			if token != "" {
				auth := request.Header.Get("Authorization")
				if auth == "Bearer "+token {
					next.ServeHTTP(writer, request)
					return
				}
				// Also check query param for WebSocket connections.
				if request.URL.Query().Get("token") == token {
					next.ServeHTTP(writer, request)
					return
				}
			}

			password := self.config.Gateway.Auth.Password
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
}

// --- Listen address ---

// ListenAddress returns the host:port string derived from configs.
func (self *gateway) ListenAddress() string {
	host := "127.0.0.1"
	if self.config.Gateway.Bind == "lan" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", self.config.Gateway.Port))
}
