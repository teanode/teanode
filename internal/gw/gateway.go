package gw

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/integrations/browsers/relaybrowser"
	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/summarizer"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/voice"
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
	ctx           context.Context
	config        *models.Configuration
	agentRegistry *agents.AgentRegistry
	browserRelay  *relaybrowser.Relay
	terminalRelay *terminals.Relay
	summarizer    *summarizer.Summarizer

	subscribersMutex sync.RWMutex
	subscribers      map[Subscriber]struct{}
	// map[voice.VoiceSubscriber]*voiceSubscriberBridge
	voiceSubscriberBridges sync.Map
	sessionsConnected      map[string]int // sessionId -> active websocket connection count

	activeRunsMutex sync.Mutex
	activeRuns      map[string]*activeRun // conversationId -> activeRun
	runIndex        map[string]string     // runId -> conversationId

	lifecycleChannel       chan LifecycleAction
	pendingLifecycleMutex  sync.Mutex
	pendingLifecycleAction *LifecycleAction
}

// --- Subsystem access ---

func (self *gateway) AgentRegistry() *agents.AgentRegistry { return self.agentRegistry }
func (self *gateway) BrowserRelay() *relaybrowser.Relay    { return self.browserRelay }
func (self *gateway) TerminalRelay() *terminals.Relay      { return self.terminalRelay }

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

func (self *gateway) defaultAgentID() string {
	defaultAgentID := "main"
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agents, err := transaction.ListAgents(ctx, nil)
		if err != nil || len(agents) == 0 {
			return nil
		}
		agentIDs := make([]string, 0, len(agents))
		for _, agent := range agents {
			if agent.ID == "main" {
				defaultAgentID = "main"
				return nil
			}
			if agent.ID != "" {
				agentIDs = append(agentIDs, agent.ID)
			}
		}
		sort.Strings(agentIDs)
		if len(agentIDs) > 0 {
			defaultAgentID = agentIDs[0]
		}
		return nil
	})
	return defaultAgentID
}

// --- Domain operations ---

// ProviderRegistry returns the provider registry from the configured default runner.
func (self *gateway) ProviderRegistry() *providers.Registry {
	runner := self.GetRunner(self.defaultAgentID())
	if runner == nil {
		return nil
	}
	return runner.Providers
}

// GetRunner returns the runner for the given agent ID.
func (self *gateway) GetRunner(agentId string) *agents.Runner {
	return self.agentRegistry.GetRunner(agentId)
}

// --- Default agent / conversation ---

func (self *gateway) EnsureDefaultConversation(userId, agentId string) string {
	return self.agentRegistry.EnsureDefaultConversation(userId, agentId)
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
	runner := self.GetRunner(agentId)
	if runner == nil {
		return
	}
	qualifiedModel := model
	if qualifiedModel == "" {
		if self.config != nil && self.config.Models != nil {
			qualifiedModel = self.config.Models.GetDefault()
		}
		_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			agent, err := transaction.GetAgent(ctx, agentId, nil)
			if err != nil || agent == nil {
				return nil
			}
			agentModel := agent.GetModel()
			if agentModel != "" {
				qualifiedModel = agentModel
			}
			return nil
		})
	}
	if qualifiedModel == "" {
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateConversation(ctx, &models.Conversation{
			ID:      conversationId,
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		return createError
	}); err != nil {
		log.Errorf("creating conversation file: %v", err)
	}
}

func (self *gateway) NewDefaultConversation(userId, agentId, model string) string {
	if userId == "" {
		log.Warningf("new conversation requires non-empty userId")
		return ""
	}
	conversationId := self.agentRegistry.NewDefaultConversation(userId, agentId)
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
	if user := models.UserFromContext(ctx); user != nil {
		userId = user.ID
	}
	if userId == "" {
		return &RunHandle{Done: closedDoneChannel(), Outcome: func() *RunOutcome {
			return &RunOutcome{Error: fmt.Errorf("userId is required")}
		}}
	}

	// Resolve agent and runner.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		if user := models.UserFromContext(ctx); user != nil {
			resolvedAgentId = user.GetDefaultAgentID()
		}
		if resolvedAgentId == "" {
			return &RunHandle{Done: closedDoneChannel(), Outcome: func() *RunOutcome {
				return &RunOutcome{Error: fmt.Errorf("cannot determine default agent")}
			}}
		}
	}
	runner := self.GetRunner(resolvedAgentId)
	if runner == nil {
		return &RunHandle{Done: closedDoneChannel(), Outcome: func() *RunOutcome {
			return &RunOutcome{Error: fmt.Errorf("agent not found: %s", resolvedAgentId)}
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

	if self.GetRunner(agentId) == nil {
		return fmt.Errorf("agent not found: %s", agentId)
	}
	deleteError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteConversation(ctx, conversationId, nil)
	})
	if deleteError != nil && deleteError != store.ErrNotFound {
		return deleteError
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
	bind := ""
	port := 8833
	if self.config != nil && self.config.Gateway != nil {
		bind = self.config.Gateway.GetBind()
		if self.config.Gateway.GetPort() > 0 {
			port = self.config.Gateway.GetPort()
		}
	}
	if bind == "lan" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

// StartVoiceSession creates a voice session bound to this gateway instance.
func (self *gateway) StartVoiceSession(
	ctx context.Context,
	conversationId, agentId string,
	promptSuffix string,
	audioIn, audioOut voice.AudioFormat,
	features voice.Features,
	sendJson func(interface{}),
	sendBinary func([]byte),
) (*voice.Session, error) {
	userId := ""
	if user := models.UserFromContext(ctx); user != nil {
		userId = user.ID
	}
	if userId == "" {
		return nil, fmt.Errorf("userId is required")
	}
	agentId = models.UserFromContext(ctx).GetDefaultAgentID()
	if agentId == "" {
		return nil, fmt.Errorf("no default agent configured")
	}
	if conversationId == "" {
		// Start a fresh conversation when the client omits conversation_id.
		// This avoids cross-session context bleed between separate voice calls.
		conversationId = security.NewULID()
		self.createConversationFile(userId, agentId, conversationId, "")
		self.SetDefaultConversationIfUnset(userId, agentId, conversationId)
	}
	sessionId := security.NewULID()
	adapterContext := ctx
	adapter := &voiceGatewayAdapter{gw: self, ctx: adapterContext}
	return voice.NewSession(sessionId, conversationId, agentId, promptSuffix, audioIn, audioOut, features, adapter, sendJson, sendBinary), nil
}

type voiceGatewayAdapter struct {
	gw  *gateway
	ctx context.Context
}

func (self *voiceGatewayAdapter) SendMessage(_ context.Context, parameters voice.VoiceSendMessageParams) voice.VoiceRunHandle {
	handle := self.gw.SendMessage(self.ctx, SendMessageParameters{
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
