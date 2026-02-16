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
	"github.com/teanode/teanode/internal/util/ulid"
	"github.com/teanode/teanode/internal/web"
	"gopkg.in/yaml.v3"
)

// activeRun tracks a running agent invocation with its cancel function.
type activeRun struct {
	runId  string
	cancel context.CancelFunc
	runner *agents.Runner
}

// gateway is the unexported concrete implementation of Gateway.
type gateway struct {
	config        *configs.Config
	agentRegistry *agents.AgentRegistry
	browserRelay  *browsers.Relay
	terminalRelay *terminals.Relay
	scheduler     *jobs.Scheduler
	summarizer    *agents.Summarizer
	mediaStore    *media.Store

	subscribersMutex sync.RWMutex
	subscribers      map[Subscriber]struct{}

	activeRunsMutex sync.Mutex
	activeRuns      map[string]*activeRun // conversationId -> activeRun
	runIndex        map[string]string     // runId -> conversationId

	modelsMutex sync.RWMutex
	models      map[string][]provider.ModelInfo // provider name -> models
	modelsTime  time.Time                       // when cache was populated
}

// --- Configuration access ---

func (self *gateway) Config() *configs.Config                 { return self.config }
func (self *gateway) SetConfig(configuration *configs.Config) { self.config = configuration }

// --- Subsystem access ---

func (self *gateway) AgentRegistry() *agents.AgentRegistry { return self.agentRegistry }
func (self *gateway) Scheduler() *jobs.Scheduler           { return self.scheduler }
func (self *gateway) Summarizer() *agents.Summarizer       { return self.summarizer }
func (self *gateway) MediaStore() *media.Store              { return self.mediaStore }
func (self *gateway) BrowserRelay() *browsers.Relay         { return self.browserRelay }
func (self *gateway) TerminalRelay() *terminals.Relay       { return self.terminalRelay }

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

func (self *gateway) ActiveAgentID() string { return self.agentRegistry.ActiveAgentID() }
func (self *gateway) ActiveConversationID(agentId string) string {
	return self.agentRegistry.ActiveConversationID(agentId)
}

func (self *gateway) SetActiveAgent(agentId string) error {
	err := self.agentRegistry.SetActiveAgent(agentId)
	if err == nil {
		self.Broadcast("activeAgent", map[string]interface{}{
			"activeAgentId":        agentId,
			"activeConversationId": self.agentRegistry.ActiveConversationID(agentId),
		})
	}
	return err
}

func (self *gateway) SetActiveConversation(agentId, conversationId string) {
	self.agentRegistry.SetActiveConversation(agentId, conversationId)
	self.Broadcast("activeConversation", map[string]interface{}{
		"agentId":              agentId,
		"activeConversationId": conversationId,
	})
}

func (self *gateway) SetActiveConversationIfUnset(agentId, conversationId string) bool {
	changed := self.agentRegistry.SetActiveConversationIfUnset(agentId, conversationId)
	if changed {
		self.Broadcast("activeConversation", map[string]interface{}{
			"agentId":              agentId,
			"activeConversationId": conversationId,
		})
	}
	return changed
}

func (self *gateway) NewConversation(agentId string) string {
	conversationId := self.agentRegistry.NewConversation(agentId)
	self.Broadcast("activeConversation", map[string]interface{}{
		"agentId":              agentId,
		"activeConversationId": conversationId,
	})
	return conversationId
}

// --- Subscriber pattern ---

// Subscribe registers a subscriber to receive broadcast events.
func (self *gateway) Subscribe(subscriber Subscriber) {
	self.subscribersMutex.Lock()
	self.subscribers[subscriber] = struct{}{}
	self.subscribersMutex.Unlock()
}

// Unsubscribe removes a subscriber.
func (self *gateway) Unsubscribe(subscriber Subscriber) {
	self.subscribersMutex.Lock()
	delete(self.subscribers, subscriber)
	self.subscribersMutex.Unlock()
}

// Broadcast sends an event to all subscribers.
func (self *gateway) Broadcast(event string, payload interface{}) {
	self.subscribersMutex.RLock()
	defer self.subscribersMutex.RUnlock()
	for subscriber := range self.subscribers {
		subscriber.OnEvent(event, payload)
	}
}

// --- Centralized message sending ---

// SendMessage orchestrates an agent run: resolves runner and conversation, generates
// a run ID, tracks the run, broadcasts all events, merges caller callbacks, and cleans
// up on completion. Returns a RunHandle immediately so the caller can wait or proceed.
func (self *gateway) SendMessage(ctx context.Context, parameters SendMessageParameters, callerCallbacks *agents.RunCallbacks) *RunHandle {
	// Resolve agent and runner.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		resolvedAgentId = self.agentRegistry.ActiveAgentID()
	}
	runner := self.ResolveRunner(resolvedAgentId)

	// Resolve or create conversation.
	conversationId := parameters.ConversationID
	if conversationId == "" {
		conversationId = self.NewConversation(resolvedAgentId)
	} else {
		self.SetActiveConversationIfUnset(resolvedAgentId, conversationId)
	}

	// Generate run ID and create cancellable context.
	runId := ulid.GenerateString()
	runContext, cancel := context.WithCancel(ctx)

	// Track the active run.
	self.activeRunsMutex.Lock()
	self.activeRuns[conversationId] = &activeRun{
		runId:  runId,
		cancel: cancel,
		runner: runner,
	}
	self.runIndex[runId] = conversationId
	self.activeRunsMutex.Unlock()

	// Broadcast user message and conversations list update.
	userMessagePayload := map[string]interface{}{
		"state":          "user_message",
		"runId":          runId,
		"conversationId": conversationId,
		"agentId":        resolvedAgentId,
		"text":           parameters.Message,
	}
	if parameters.OriginID != "" {
		userMessagePayload["originId"] = parameters.OriginID
	}
	if parameters.Origin != "" {
		userMessagePayload["origin"] = parameters.Origin
	}
	self.Broadcast("conversation", userMessagePayload)
	self.Broadcast("conversations", nil)

	// Prepare the outcome channel.
	done := make(chan struct{})
	var outcome RunOutcome

	// Build merged callbacks (broadcast + caller).
	mergedCallbacks := self.buildMergedCallbacks(runId, conversationId, resolvedAgentId, callerCallbacks)

	// Run agent in background goroutine.
	go func() {
		defer close(done)
		defer func() {
			// Clean up run tracking.
			self.activeRunsMutex.Lock()
			if current, exists := self.activeRuns[conversationId]; exists && current.runId == runId {
				delete(self.activeRuns, conversationId)
			}
			delete(self.runIndex, runId)
			self.activeRunsMutex.Unlock()
			cancel()

			// Notify summarizer.
			if self.summarizer != nil {
				self.summarizer.Notify()
			}
		}()

		result, err := runner.Run(runContext, agents.RunParams{
			ConversationID: conversationId,
			Message:        parameters.Message,
			Model:          parameters.Model,
		}, mergedCallbacks)

		if err != nil {
			outcome.Error = err
			if runContext.Err() != nil {
				self.Broadcast("conversation", map[string]interface{}{
					"state":          "aborted",
					"runId":          runId,
					"conversationId": conversationId,
					"agentId":        resolvedAgentId,
				})
			} else {
				self.Broadcast("conversation", map[string]interface{}{
					"state":          "error",
					"runId":          runId,
					"conversationId": conversationId,
					"agentId":        resolvedAgentId,
					"error":          err.Error(),
				})
			}
			return
		}

		outcome.Response = result.Response
		outcome.Model = result.Model
		outcome.StopReason = result.StopReason
		outcome.Usage = result.Usage

		payload := map[string]interface{}{
			"state":          "final",
			"runId":          runId,
			"conversationId": conversationId,
			"agentId":        resolvedAgentId,
			"text":           result.Response,
			"model":          result.Model,
			"stopReason":     result.StopReason,
		}
		if result.Usage != nil {
			payload["usage"] = result.Usage
		}
		self.Broadcast("conversation", payload)
	}()

	return &RunHandle{
		RunID:          runId,
		ConversationID: conversationId,
		Done:           done,
		Outcome:        func() *RunOutcome { return &outcome },
	}
}

// buildMergedCallbacks creates RunCallbacks that both broadcast events (using the
// "conversation" event name consistently) and call the caller's optional callbacks.
func (self *gateway) buildMergedCallbacks(runId, conversationId, agentId string, callerCallbacks *agents.RunCallbacks) *agents.RunCallbacks {
	return &agents.RunCallbacks{
		OnQueued: func() {
			self.Broadcast("conversation", map[string]interface{}{
				"state":          "queued",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
			})
			if callerCallbacks != nil && callerCallbacks.OnQueued != nil {
				callerCallbacks.OnQueued()
			}
		},
		OnStart: func() {
			if callerCallbacks != nil && callerCallbacks.OnStart != nil {
				callerCallbacks.OnStart()
			}
		},
		OnTextDelta: func(text string) {
			self.Broadcast("conversation", map[string]interface{}{
				"state":          "delta",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"text":           text,
			})
			if callerCallbacks != nil && callerCallbacks.OnTextDelta != nil {
				callerCallbacks.OnTextDelta(text)
			}
		},
		OnToolCall: func(toolName string, arguments string) {
			self.Broadcast("conversation", map[string]interface{}{
				"state":          "tool_call",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"toolName":       toolName,
				"arguments":      arguments,
			})
			if callerCallbacks != nil && callerCallbacks.OnToolCall != nil {
				callerCallbacks.OnToolCall(toolName, arguments)
			}
		},
		OnToolResult: func(toolName string, result string) {
			self.Broadcast("conversation", map[string]interface{}{
				"state":          "tool_result",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"toolName":       toolName,
				"result":         result,
			})
			if callerCallbacks != nil && callerCallbacks.OnToolResult != nil {
				callerCallbacks.OnToolResult(toolName, result)
			}
		},
	}
}

// --- Run tracking and abort ---

// AbortRun cancels a running agent invocation by run ID. Returns true if the run was found.
func (self *gateway) AbortRun(runId string) bool {
	self.activeRunsMutex.Lock()
	conversationId, found := self.runIndex[runId]
	if !found {
		self.activeRunsMutex.Unlock()
		return false
	}
	run, exists := self.activeRuns[conversationId]
	self.activeRunsMutex.Unlock()

	if !exists {
		return false
	}

	run.cancel()
	if run.runner != nil {
		run.runner.CancelConversation(conversationId)
	}
	return true
}

// GetActiveRun returns the active run ID for a conversation, or empty string if none.
func (self *gateway) GetActiveRun(conversationId string) string {
	self.activeRunsMutex.Lock()
	defer self.activeRunsMutex.Unlock()
	if run, exists := self.activeRuns[conversationId]; exists {
		return run.runId
	}
	return ""
}

// --- Delete conversation ---

// DeleteConversation deletes a conversation if it's not actively running.
func (self *gateway) DeleteConversation(agentId, conversationId string) error {
	// Check not active.
	if self.GetActiveRun(conversationId) != "" {
		return fmt.Errorf("cannot delete conversation with active run")
	}

	runner := self.ResolveRunner(agentId)
	if runner == nil {
		return fmt.Errorf("agent not found: %s", agentId)
	}

	if err := runner.Conversations.Delete(conversationId); err != nil {
		return err
	}

	self.Broadcast("conversations", nil)
	return nil
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

			web.WriteError(writer, web.ErrUnauthorized)
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
