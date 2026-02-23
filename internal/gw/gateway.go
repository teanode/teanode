package gw

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/media"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/sessions"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/voice"
	"gopkg.in/yaml.v3"
)

// activeRun tracks a running agent invocation with its cancel function.
type activeRun struct {
	runId  string
	cancel context.CancelFunc
	runner *agents.Runner
}

func closedDoneChannel() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// gateway is the unexported concrete implementation of Gateway.
type gateway struct {
	config         *configs.Config
	securityConfig *configs.SecurityConfig
	agentRegistry  *agents.AgentRegistry
	browserRelay   *relaybrowser.Relay
	terminalRelay  *terminals.Relay
	scheduler      *jobs.Scheduler
	summarizer     *agents.Summarizer
	mediaStore     *media.Store
	sessionStore   *sessions.Store

	subscribersMutex sync.RWMutex
	subscribers      map[Subscriber]struct{}
	// map[voice.VoiceSubscriber]*voiceSubscriberBridge
	voiceSubscriberBridges sync.Map
	sessionsConnected      map[string]int // sessionId -> active websocket connection count

	activeRunsMutex sync.Mutex
	activeRuns      map[string]*activeRun // conversationId -> activeRun
	runIndex        map[string]string     // runId -> conversationId

	modelsMutex sync.RWMutex
	models      map[string][]providers.ModelInfo // provider name -> models
	modelsTime  time.Time                        // when cache was populated

	lifecycleChannel       chan LifecycleAction
	pendingLifecycleMutex  sync.Mutex
	pendingLifecycleAction *LifecycleAction

	conversationStoresMutex sync.Mutex
	conversationStores      map[string]*conversations.Store // userId:agentId -> store
}

// --- Configuration access ---

func (self *gateway) Config() *configs.Config                 { return self.config }
func (self *gateway) SetConfig(configuration *configs.Config) { self.config = configuration }
func (self *gateway) SecurityConfig() *configs.SecurityConfig { return self.securityConfig }
func (self *gateway) SetSecurityConfig(securityConfig *configs.SecurityConfig) {
	self.securityConfig = securityConfig
}

// --- Subsystem access ---

func (self *gateway) AgentRegistry() *agents.AgentRegistry { return self.agentRegistry }
func (self *gateway) Scheduler() *jobs.Scheduler           { return self.scheduler }
func (self *gateway) Summarizer() *agents.Summarizer       { return self.summarizer }
func (self *gateway) MediaStore() *media.Store             { return self.mediaStore }
func (self *gateway) BrowserRelay() *relaybrowser.Relay    { return self.browserRelay }
func (self *gateway) TerminalRelay() *terminals.Relay      { return self.terminalRelay }
func (self *gateway) SessionStore() *sessions.Store        { return self.sessionStore }

func (self *gateway) MarkSessionConnected(sessionId string) {
	if sessionId == "" {
		return
	}
	self.subscribersMutex.Lock()
	self.sessionsConnected[sessionId]++
	self.subscribersMutex.Unlock()
}

func (self *gateway) MarkSessionDisconnected(sessionId string) {
	if sessionId == "" {
		return
	}
	self.subscribersMutex.Lock()
	if count, ok := self.sessionsConnected[sessionId]; ok {
		if count <= 1 {
			delete(self.sessionsConnected, sessionId)
		} else {
			self.sessionsConnected[sessionId] = count - 1
		}
	}
	self.subscribersMutex.Unlock()
}

func (self *gateway) IsSessionConnected(sessionId string) bool {
	if sessionId == "" {
		return false
	}
	self.subscribersMutex.RLock()
	defer self.subscribersMutex.RUnlock()
	return self.sessionsConnected[sessionId] > 0
}

// --- Domain operations ---

// ProviderRegistry returns the provider registry from the default runner.
func (self *gateway) ProviderRegistry() *providers.Registry {
	runner := self.agentRegistry.Default()
	if runner == nil {
		return nil
	}
	_, providerRegistry, _, _, _ := runner.Snapshot()
	return providerRegistry
}

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

func conversationStoreKey(userId, agentId string) string {
	return userId + ":" + agentId
}

func (self *gateway) ConversationStore(userId, agentId string) *conversations.Store {
	if userId == "" {
		log.Warningf("conversation store requires non-empty userId")
		return nil
	}
	key := conversationStoreKey(userId, agentId)
	self.conversationStoresMutex.Lock()
	defer self.conversationStoresMutex.Unlock()
	if store, ok := self.conversationStores[key]; ok {
		return store
	}
	directory, err := configs.UserAgentConversationsDirectory(userId, agentId)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil
	}
	store := conversations.NewStore(directory)
	self.conversationStores[key] = store
	return store
}

// modelsCache is the YAML structure written to ~/.teanode/models.yaml.
type modelsCache struct {
	FetchedAt time.Time                        `json:"fetchedAt" yaml:"fetchedAt"`
	Providers map[string][]providers.ModelInfo `json:"providers" yaml:"providers"`
}

const modelsCacheMaxAge = 24 * time.Hour

// LoadModels returns the cached models or fetches from each provider's API.
func (self *gateway) LoadModels(ctx context.Context) (map[string][]providers.ModelInfo, error) {
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

	// Use the default runner to resolve the currently configured providers.
	defaultRunner := self.agentRegistry.Default()
	if defaultRunner == nil {
		return nil, fmt.Errorf("no default agent runner")
	}
	_, providerRegistry, _, _, _ := defaultRunner.Snapshot()
	providerNames := providerRegistry.ProviderNames()

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

	// Fetch from each provider's API.
	result := make(map[string][]providers.ModelInfo)
	for _, name := range providerNames {
		provider, _, err := providerRegistry.Resolve(providers.QualifyModel(name, "dummy"))
		if err != nil {
			continue
		}
		models, err := provider.ListModels(ctx)
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

func (self *gateway) updateRunnerContextWindows(models map[string][]providers.ModelInfo) {
	self.agentRegistry.ForEach(func(agentId string, runner *agents.Runner) {
		for providerName, modelList := range models {
			runner.SetModels(providerName, modelList)
		}
	})
}

// --- Default agent / conversation ---

func (self *gateway) DefaultAgentID() string { return self.agentRegistry.DefaultID() }
func (self *gateway) DefaultConversationID(userId, agentId string) string {
	return self.agentRegistry.DefaultConversationID(userId, agentId)
}

func (self *gateway) SetDefaultAgent(agentId string) error {
	err := self.agentRegistry.SetDefaultAgent(agentId)
	if err == nil {
		self.Broadcast(EventTypeDefaultAgent, map[string]interface{}{
			"defaultAgentId": agentId,
		})
	}
	return err
}

func (self *gateway) SetDefaultConversation(userId, agentId, conversationId string) {
	if userId == "" {
		log.Warningf("set default conversation requires non-empty userId")
		return
	}
	self.agentRegistry.SetDefaultConversation(userId, agentId, conversationId)
	self.Broadcast(EventTypeDefaultConversation, map[string]interface{}{
		"agentId":               agentId,
		"defaultConversationId": conversationId,
		"userId":                userId,
	})
}

func (self *gateway) SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool {
	if userId == "" {
		log.Warningf("set default conversation-if-unset requires non-empty userId")
		return false
	}
	changed := self.agentRegistry.SetDefaultConversationIfUnset(userId, agentId, conversationId)
	if changed {
		self.Broadcast(EventTypeDefaultConversation, map[string]interface{}{
			"agentId":               agentId,
			"defaultConversationId": conversationId,
			"userId":                userId,
		})
	}
	return changed
}

func (self *gateway) createConversationFile(userId, agentId, conversationId, model string) {
	if userId == "" {
		return
	}
	// Resolve model and create conversation file with provider/model in the header.
	runner := self.ResolveRunner(agentId)
	if runner == nil {
		return
	}
	qualifiedModel := model
	if qualifiedModel == "" {
		qualifiedModel = self.config.AgentModel(agentId)
	}
	if qualifiedModel == "" {
		return
	}
	_, providerRegistry, _, _, _ := runner.Snapshot()
	resolvedProvider, _ := providers.ParseQualifiedModel(qualifiedModel, providerRegistry.DefaultProvider())
	store := self.ConversationStore(userId, agentId)
	if store == nil {
		return
	}
	if err := store.Create(conversationId, resolvedProvider, qualifiedModel); err != nil {
		log.Errorf("creating conversation file: %v", err)
	}
}

func (self *gateway) NewConversation(userId, agentId, model string) string {
	if userId == "" {
		log.Warningf("new conversation requires non-empty userId")
		return ""
	}
	conversationId := self.agentRegistry.NewConversation(userId, agentId)
	self.createConversationFile(userId, agentId, conversationId, model)

	self.Broadcast(EventTypeDefaultConversation, map[string]interface{}{
		"agentId":               agentId,
		"defaultConversationId": conversationId,
		"userId":                userId,
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
func (self *gateway) Broadcast(eventType EventType, payload interface{}) {
	self.subscribersMutex.RLock()
	defer self.subscribersMutex.RUnlock()
	for subscriber := range self.subscribers {
		subscriber.OnEvent(eventType, payload)
	}
}

// --- Centralized message sending ---

// SendMessage orchestrates an agent run: resolves runner and conversation, generates
// a run ID, tracks the run, broadcasts all events, merges caller callbacks, and cleans
// up on completion. Returns a RunHandle immediately so the caller can wait or proceed.
func (self *gateway) SendMessage(ctx context.Context, parameters SendMessageParameters, callerCallbacks *agents.RunCallbacks) *RunHandle {
	userId := ""
	if parameters.UserContext != nil {
		userId = parameters.UserContext.UserID
	}
	if userId == "" {
		return &RunHandle{Done: closedDoneChannel(), Outcome: func() *RunOutcome {
			return &RunOutcome{Error: fmt.Errorf("userId is required")}
		}}
	}

	// Resolve agent and runner.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		resolvedAgentId = self.agentRegistry.DefaultID()
	}
	runner := self.ResolveRunner(resolvedAgentId)
	if runner == nil {
		return &RunHandle{Done: closedDoneChannel(), Outcome: func() *RunOutcome {
			return &RunOutcome{Error: fmt.Errorf("agent not found: %s", resolvedAgentId)}
		}}
	}
	if self.ConversationStore(userId, resolvedAgentId) == nil {
		return &RunHandle{Done: closedDoneChannel(), Outcome: func() *RunOutcome {
			return &RunOutcome{Error: fmt.Errorf("conversation store not available")}
		}}
	}

	// Resolve or create conversation.
	conversationId := parameters.ConversationID
	if conversationId == "" {
		conversationId = security.NewULID()
		self.createConversationFile(userId, resolvedAgentId, conversationId, parameters.Model)
		self.SetDefaultConversationIfUnset(userId, resolvedAgentId, conversationId)
	} else {
		self.SetDefaultConversationIfUnset(userId, resolvedAgentId, conversationId)
	}

	// Generate run ID and create cancellable context.
	runId := security.NewULID()
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
		"userId":         userId,
		"text":           parameters.Message,
	}
	if parameters.OriginID != "" {
		userMessagePayload["originId"] = parameters.OriginID
	}
	if parameters.Origin != "" {
		userMessagePayload["origin"] = parameters.Origin
	}
	if parameters.OriginSessionID != "" {
		userMessagePayload["originSessionId"] = parameters.OriginSessionID
	}
	if len(parameters.Attachments) > 0 {
		userMessagePayload["attachments"] = parameters.Attachments
	}
	self.Broadcast(EventTypeConversation, userMessagePayload)
	self.Broadcast(EventTypeConversations, nil)

	// Prepare the outcome channel.
	done := make(chan struct{})
	var outcome RunOutcome

	// Build merged callbacks (broadcast + caller).
	mergedCallbacks := self.buildMergedCallbacks(runId, conversationId, resolvedAgentId, userId, callerCallbacks)

	// Run agent in background goroutine.
	go func() {
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

			// Fire any deferred lifecycle action now that the run is complete.
			self.firePendingLifecycle()
		}()
		// Signal run completion to callers before cleanup may trigger a deferred
		// lifecycle action (restart/shutdown). Channel callers (e.g. Telegram)
		// use this to flush the final response before process restart.
		defer close(done)

		isAdmin := false
		if self.securityConfig != nil {
			isAdmin = self.securityConfig.IsAdmin(userId)
		}
		runContext = agents.ContextWithUserID(runContext, userId)
		runContext = agents.ContextWithAdmin(runContext, isAdmin)
		result, err := runner.Run(runContext, agents.RunParams{
			ConversationID:     conversationId,
			Message:            parameters.Message,
			Model:              parameters.Model,
			Attachments:        parameters.Attachments,
			SystemPromptSuffix: parameters.SystemPromptSuffix,
		}, mergedCallbacks)

		if err != nil {
			outcome.Error = err
			if runContext.Err() != nil {
				self.Broadcast(EventTypeConversation, map[string]interface{}{
					"state":          "aborted",
					"runId":          runId,
					"conversationId": conversationId,
					"agentId":        resolvedAgentId,
					"userId":         userId,
				})
			} else {
				self.Broadcast(EventTypeConversation, map[string]interface{}{
					"state":          "error",
					"runId":          runId,
					"conversationId": conversationId,
					"agentId":        resolvedAgentId,
					"userId":         userId,
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
			"userId":         userId,
			"text":           result.Response,
			"model":          result.Model,
			"stopReason":     result.StopReason,
		}
		if result.Usage != nil {
			payload["usage"] = result.Usage
		}
		if result.ContextWindow > 0 {
			payload["contextWindow"] = result.ContextWindow
		}
		self.Broadcast(EventTypeConversation, payload)
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
func (self *gateway) buildMergedCallbacks(runId, conversationId, agentId, userId string, callerCallbacks *agents.RunCallbacks) *agents.RunCallbacks {
	// Notify summarizer on first text delta so untitled conversations get a title
	// while tool-call loops are still running.
	var notifyOnce sync.Once

	return &agents.RunCallbacks{
		OnQueued: func() {
			self.Broadcast(EventTypeConversation, map[string]interface{}{
				"state":          "queued",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"userId":         userId,
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
			self.Broadcast(EventTypeConversation, map[string]interface{}{
				"state":          "delta",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"userId":         userId,
				"text":           text,
			})
			if self.summarizer != nil {
				notifyOnce.Do(func() { self.summarizer.Notify() })
			}
			if callerCallbacks != nil && callerCallbacks.OnTextDelta != nil {
				callerCallbacks.OnTextDelta(text)
			}
		},
		OnToolCall: func(toolName string, arguments string) {
			self.Broadcast(EventTypeConversation, map[string]interface{}{
				"state":          "tool_call",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"userId":         userId,
				"toolName":       toolName,
				"arguments":      arguments,
			})
			if callerCallbacks != nil && callerCallbacks.OnToolCall != nil {
				callerCallbacks.OnToolCall(toolName, arguments)
			}
		},
		OnToolResult: func(toolName string, result string) {
			self.Broadcast(EventTypeConversation, map[string]interface{}{
				"state":          "tool_result",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        agentId,
				"userId":         userId,
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
func (self *gateway) DeleteConversation(userId, agentId, conversationId string) error {
	// Check not active.
	if self.GetActiveRun(conversationId) != "" {
		return fmt.Errorf("cannot delete conversation with active run")
	}

	if self.ResolveRunner(agentId) == nil {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	store := self.ConversationStore(userId, agentId)
	if store == nil {
		return fmt.Errorf("conversation store not found")
	}

	if err := store.Delete(conversationId); err != nil {
		return err
	}

	self.Broadcast(EventTypeConversations, nil)
	return nil
}

// --- Lifecycle controls ---

// RequestLifecycle sends a lifecycle action immediately (non-blocking).
// Used by slash commands that run outside agent runs.
func (self *gateway) RequestLifecycle(action LifecycleAction) {
	select {
	case self.lifecycleChannel <- action:
	default:
	}
}

// ScheduleLifecycle stores a pending lifecycle action that fires after the
// current agent run completes. Used by the LLM gateway tool so the conversation
// finishes cleanly before the gateway shuts down or restarts.
func (self *gateway) ScheduleLifecycle(action LifecycleAction) {
	self.pendingLifecycleMutex.Lock()
	self.pendingLifecycleAction = &action
	self.pendingLifecycleMutex.Unlock()
}

// firePendingLifecycle checks for a pending lifecycle action and fires it.
// Called from run cleanup after the LLM response has been broadcast.
func (self *gateway) firePendingLifecycle() {
	self.pendingLifecycleMutex.Lock()
	action := self.pendingLifecycleAction
	self.pendingLifecycleAction = nil
	self.pendingLifecycleMutex.Unlock()

	if action != nil {
		self.RequestLifecycle(*action)
	}
}

func (self *gateway) LifecycleChannel() <-chan LifecycleAction {
	return self.lifecycleChannel
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

// StartVoiceSession creates a voice session bound to this gateway instance.
func (self *gateway) StartVoiceSession(
	userId, conversationId, agentId string,
	promptSuffix string,
	audioIn, audioOut voice.AudioFormat,
	features voice.Features,
	sendJson func(interface{}),
	sendBinary func([]byte),
) (*voice.Session, error) {
	if userId == "" {
		return nil, fmt.Errorf("userId is required")
	}
	if agentId == "" {
		agentId = self.DefaultAgentID()
	}
	if conversationId == "" {
		// Start a fresh conversation when the client omits conversation_id.
		// This avoids cross-session context bleed between separate voice calls.
		conversationId = security.NewULID()
		self.createConversationFile(userId, agentId, conversationId, "")
		self.SetDefaultConversationIfUnset(userId, agentId, conversationId)
	}
	sessionId := security.NewULID()
	adapter := &voiceGatewayAdapter{gw: self, userId: userId}
	return voice.NewSession(sessionId, conversationId, agentId, promptSuffix, audioIn, audioOut, features, adapter, sendJson, sendBinary), nil
}

type voiceGatewayAdapter struct {
	gw     *gateway
	userId string
}

func (self *voiceGatewayAdapter) SendMessage(ctx context.Context, parameters voice.VoiceSendMessageParams) voice.VoiceRunHandle {
	handle := self.gw.SendMessage(ctx, SendMessageParameters{
		UserContext:        &UserContext{UserID: self.userId},
		AgentID:            parameters.AgentID,
		ConversationID:     parameters.ConversationID,
		Message:            parameters.Message,
		Model:              parameters.Model,
		SystemPromptSuffix: parameters.SystemPromptSuffix,
		Origin:             "voice",
	}, nil)
	if handle == nil {
		done := make(chan struct{})
		close(done)
		return voice.VoiceRunHandle{Done: done}
	}
	return voice.VoiceRunHandle{
		RunID:          handle.RunID,
		ConversationID: handle.ConversationID,
		Done:           handle.Done,
	}
}

func (self *voiceGatewayAdapter) AbortRun(runId string) bool {
	return self.gw.AbortRun(runId)
}

func (self *voiceGatewayAdapter) Subscribe(sub voice.VoiceSubscriber) {
	bridge := &voiceSubscriberBridge{sub: sub}
	self.gw.voiceSubscriberBridges.Store(sub, bridge)
	self.gw.Subscribe(bridge)
}

func (self *voiceGatewayAdapter) Unsubscribe(sub voice.VoiceSubscriber) {
	value, ok := self.gw.voiceSubscriberBridges.LoadAndDelete(sub)
	if !ok {
		return
	}
	bridge, ok := value.(*voiceSubscriberBridge)
	if !ok {
		return
	}
	self.gw.Unsubscribe(bridge)
}

func (self *voiceGatewayAdapter) NewConversation(agentId, model string) string {
	return self.gw.NewConversation(self.userId, agentId, model)
}

func (self *voiceGatewayAdapter) DefaultAgentID() string {
	return self.gw.DefaultAgentID()
}

func (self *voiceGatewayAdapter) ProviderRegistry() voice.VoiceProviderRegistry {
	reg := self.gw.ProviderRegistry()
	if reg == nil {
		return nil
	}
	return &voiceProviderRegistryAdapter{registry: reg}
}

type voiceSubscriberBridge struct {
	sub voice.VoiceSubscriber
}

func (self *voiceSubscriberBridge) OnEvent(et EventType, payload interface{}) {
	self.sub.OnVoiceEvent(string(et), payload)
}

type voiceProviderRegistryAdapter struct {
	registry *providers.Registry
}

func (self *voiceProviderRegistryAdapter) FindTranscriber() (voice.VoiceTranscriber, string, bool) {
	transcriber, provider, ok := self.registry.FindTranscriber()
	if !ok {
		return nil, "", false
	}
	return &voiceTranscriberAdapter{transcriber: transcriber}, provider, true
}

func (self *voiceProviderRegistryAdapter) FindSynthesizer() (voice.VoiceSynthesizer, string, bool) {
	synth, provider, ok := self.registry.FindSynthesizer()
	if !ok {
		return nil, "", false
	}
	return &voiceSynthesizerAdapter{synthesizer: synth}, provider, true
}

type voiceTranscriberAdapter struct {
	transcriber providers.AudioTranscriber
}

func (self *voiceTranscriberAdapter) Transcribe(ctx context.Context, request voice.VoiceTranscribeRequest) (*voice.VoiceTranscribeResponse, error) {
	result, err := self.transcriber.Transcribe(ctx, providers.TranscribeRequest{
		Audio:    bytes.NewReader(request.Audio),
		Format:   request.Format,
		Language: request.Language,
		Prompt:   request.Prompt,
	})
	if err != nil {
		return nil, err
	}
	return &voice.VoiceTranscribeResponse{Text: result.Text}, nil
}

type voiceSynthesizerAdapter struct {
	synthesizer providers.AudioSynthesizer
}

func (self *voiceSynthesizerAdapter) SynthesizePCM(ctx context.Context, text, voiceName string, _ int) ([]byte, error) {
	result, err := self.synthesizer.Synthesize(ctx, providers.SynthesizeRequest{
		Text:   text,
		Voice:  voiceName,
		Format: "wav",
		Speed:  1.0,
	})
	if err != nil {
		return nil, err
	}
	defer result.Audio.Close()

	wavData, err := io.ReadAll(result.Audio)
	if err != nil {
		return nil, err
	}
	return wavToPCM16LE(wavData)
}

func wavToPCM16LE(wavData []byte) ([]byte, error) {
	if len(wavData) < 44 {
		return nil, fmt.Errorf("wav payload too short")
	}
	if string(wavData[0:4]) != "RIFF" || string(wavData[8:12]) != "WAVE" {
		return nil, fmt.Errorf("invalid wav header")
	}
	var (
		audioFormat   uint16
		channels      uint16
		bitsPerSample uint16
	)
	for i := 12; i+8 <= len(wavData); {
		chunkId := string(wavData[i : i+4])
		chunkSize := int(binary.LittleEndian.Uint32(wavData[i+4 : i+8]))
		chunkStart := i + 8
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(wavData) {
			break
		}
		if chunkId == "fmt " && chunkSize >= 16 {
			audioFormat = binary.LittleEndian.Uint16(wavData[chunkStart : chunkStart+2])
			channels = binary.LittleEndian.Uint16(wavData[chunkStart+2 : chunkStart+4])
			bitsPerSample = binary.LittleEndian.Uint16(wavData[chunkStart+14 : chunkStart+16])
		}
		if chunkId == "data" {
			if audioFormat != 1 {
				return nil, fmt.Errorf("unsupported wav format: %d", audioFormat)
			}
			if channels != 1 {
				return nil, fmt.Errorf("unsupported wav channels: %d", channels)
			}
			if bitsPerSample != 16 {
				return nil, fmt.Errorf("unsupported wav bits per sample: %d", bitsPerSample)
			}
			return append([]byte(nil), wavData[chunkStart:chunkEnd]...), nil
		}
		i = chunkEnd
		if i%2 == 1 {
			i++
		}
	}

	// Fallback parser: some providers return non-standard RIFF chunk sizes.
	dataOffset := 12
	for dataOffset+8 <= len(wavData) {
		idx := bytes.Index(wavData[dataOffset:], []byte("data"))
		if idx < 0 {
			break
		}
		header := dataOffset + idx
		if header+8 > len(wavData) {
			break
		}
		chunkSize := int(binary.LittleEndian.Uint32(wavData[header+4 : header+8]))
		chunkStart := header + 8
		if chunkStart >= len(wavData) {
			break
		}
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(wavData) {
			chunkEnd = len(wavData)
		}
		pcm := append([]byte(nil), wavData[chunkStart:chunkEnd]...)
		if len(pcm)%2 == 1 {
			pcm = pcm[:len(pcm)-1]
		}
		if len(pcm) > 0 {
			return pcm, nil
		}
		dataOffset = chunkStart
	}

	return nil, fmt.Errorf("wav data chunk not found")
}
