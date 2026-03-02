// Package coordinators implements runner lifecycle management and message routing.
package coordinators

import (
	"context"
	"fmt"
	"sync"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/summarizers"
	"github.com/teanode/teanode/internal/tools/askuser"
	"github.com/teanode/teanode/internal/tools/tab"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/security"
)

var log = logging.MustGetLogger("coordinator")

// Coordinator owns runner creation, the active runner map, message routing,
// default conversation management, and event broadcasting via pubsub.
// It implements RunCoordinator.
type Coordinator struct {
	ctx                        context.Context
	configuration              *models.Configuration
	providerRegistry           *providers.ProviderRegistry
	summarizer                 *summarizers.Summarizer
	pubsub                     *pubsub.PubSub
	questionBroker             *askuser.QuestionBroker
	tabToolBroker              *tab.TabToolBroker
	activeRunners              sync.Map // conversationId -> *conversationRunner
	activeRunIdConversationIds sync.Map // runId -> conversationId
	activeConversationIdRunIds sync.Map // conversationId -> runId
}

type conversationRunner struct {
	mutex      sync.Mutex
	runner     *runners.Runner
	queue      []*queuedMessage
	processing bool
	cancel     context.CancelFunc
	aborted    bool
}

type queuedMessage struct {
	ctx        context.Context
	agentId    string
	parameters runners.RunParameters
	callbacks  *runners.RunCallbacks
	resultChan chan<- messageResult
	compact    bool // true for compact operations
}

type messageResult struct {
	runResult     *runners.RunResult
	compactResult *runners.CompactResult
	err           error
	aborted       bool
}

// New creates a Coordinator.
func New(
	ctx context.Context,
	configuration *models.Configuration,
	providerRegistry *providers.ProviderRegistry,
	summarizerInstance *summarizers.Summarizer,
	events *pubsub.PubSub,
) *Coordinator {
	return &Coordinator{
		ctx:              ctx,
		configuration:    configuration,
		providerRegistry: providerRegistry,
		summarizer:       summarizerInstance,
		pubsub:           events,
		questionBroker:   askuser.NewQuestionBroker(),
		tabToolBroker:    tab.NewTabToolBroker(),
	}
}

// ProviderRegistry returns the provider registry.
func (self *Coordinator) ProviderRegistry() *providers.ProviderRegistry {
	return self.providerRegistry
}

// QuestionBroker returns the in-memory question broker.
func (self *Coordinator) QuestionBroker() *askuser.QuestionBroker {
	return self.questionBroker
}

// TabToolBroker returns the in-memory tab tool broker.
func (self *Coordinator) TabToolBroker() *tab.TabToolBroker {
	return self.tabToolBroker
}

// Run orchestrates an agent run: resolves conversation, generates
// a runner ID, tracks the run, broadcasts all events, merges caller callbacks, and cleans
// up on completion. Returns a RunHandle immediately so the caller can wait or proceed.
func (self *Coordinator) Run(ctx context.Context, parameters RunParameters, callerCallbacks *runners.RunCallbacks) (*RunHandle, error) {
	userId := ""
	if user := models.UserFromContext(ctx); user != nil {
		userId = user.ID
	}
	if userId == "" {
		return nil, fmt.Errorf("userId is required")
	}

	// Resolve agent.
	resolvedAgentId := parameters.AgentID
	if resolvedAgentId == "" {
		if user := models.UserFromContext(ctx); user != nil {
			resolvedAgentId = user.GetDefaultAgentID()
		}
		if resolvedAgentId == "" {
			return nil, fmt.Errorf("cannot determine default agent")
		}
	}

	// Verify agent exists in store.
	var agentExists bool
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		agent, err := transaction.GetAgent(ctx, resolvedAgentId, nil)
		if err == nil && agent != nil {
			agentExists = true
		}
		return nil
	})
	if !agentExists {
		return nil, fmt.Errorf("agent not found: %s", resolvedAgentId)
	}

	// Resolve or create conversation.
	conversationId := parameters.ConversationID
	if conversationId == "" {
		conversationId = security.NewULID()
		self.createConversation(userId, resolvedAgentId, conversationId)
	}
	self.SetDefaultConversationIfUnset(userId, resolvedAgentId, conversationId)

	// Generate runner ID and create cancellable context.
	runId := security.NewULID()
	runContext, cancel := context.WithCancel(ctx)

	// Track runner in index.
	self.activeRunIdConversationIds.Store(runId, conversationId)
	self.activeConversationIdRunIds.Store(conversationId, runId)

	// Create handle.
	handle := NewRunHandle(runId, conversationId)

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
	self.pubsub.Broadcast(pubsub.EventTypeConversation, userMessagePayload)
	self.pubsub.Broadcast(pubsub.EventTypeConversations, nil)

	// Build merged callbacks (broadcast + caller).
	mergedCallbacks := self.buildMergedCallbacks(runId, conversationId, resolvedAgentId, userId, callerCallbacks)

	// Ensure coordinator is on context.
	runContext = ContextWithCoordinator(runContext, self)

	// Run agent in background goroutine via internal dispatch.
	go func() {
		defer deferutil.Recover()
		defer func() {
			// Safety net: ensure the handle is always resolved even if the
			// goroutine panics before reaching the normal Resolve calls.
			// RunHandle.Resolve is sync.Once-protected so this is a no-op
			// when the handle was already resolved normally.
			handle.Resolve(nil, fmt.Errorf("run terminated unexpectedly"))
		}()
		defer func() {
			// Clean up runner index.
			self.activeRunIdConversationIds.Delete(runId)
			// Only remove conversation→run mapping if it still points to this run,
			// to avoid clobbering a newer run's entry.
			self.activeConversationIdRunIds.CompareAndDelete(conversationId, runId)
			cancel()

			// Notify summarizer.
			if self.summarizer != nil {
				self.summarizer.Notify()
			}

			// Fire any deferred lifecycle action now that the run is complete.
			if lifecycleManager := self.lifecycle(); lifecycleManager != nil {
				lifecycleManager.FirePendingLifecycle()
			}
		}()

		dispatch := self.dispatchMessage(runContext, resolvedAgentId, conversationId, runners.RunParameters{
			Message:            parameters.Message,
			ProviderModelName:  parameters.ProviderModelName,
			Attachments:        parameters.Attachments,
			SystemPromptSuffix: parameters.SystemPromptSuffix,
			SystemPromptMode:   parameters.SystemPromptMode,
			Origin:             parameters.Origin,
		}, mergedCallbacks)

		if dispatch.aborted {
			log.Warningf("run aborted run %q conversation %q err=%v", runId, conversationId, dispatch.err)
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
				"state":          "aborted",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        resolvedAgentId,
				"userId":         userId,
			})
			handle.Resolve(nil, dispatch.err)
			return
		}

		if dispatch.err != nil {
			log.Errorf("run error run %q conversation %q: %v", runId, conversationId, dispatch.err)
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
				"state":          "error",
				"runId":          runId,
				"conversationId": conversationId,
				"agentId":        resolvedAgentId,
				"userId":         userId,
				"error":          dispatch.err.Error(),
			})
			handle.Resolve(nil, dispatch.err)
			return
		}

		result := dispatch.runResult
		payload := map[string]interface{}{
			"state":             "final",
			"runId":             runId,
			"conversationId":    conversationId,
			"agentId":           resolvedAgentId,
			"userId":            userId,
			"text":              result.Response,
			"providerModelName": result.ProviderModelName,
			"stopReason":        result.StopReason,
		}
		if result.Usage != nil {
			payload["usage"] = result.Usage
		}
		if result.ContextWindow > 0 {
			payload["contextWindow"] = result.ContextWindow
		}
		self.pubsub.Broadcast(pubsub.EventTypeConversation, payload)
		handle.Resolve(result, nil)
	}()

	return handle, nil
}

// buildMergedCallbacks creates RunCallbacks that both broadcast events and call the caller's optional callbacks.
func (self *Coordinator) buildMergedCallbacks(runId, conversationId, agentId, userId string, callerCallbacks *runners.RunCallbacks) *runners.RunCallbacks {
	var notifyOnce sync.Once

	return &runners.RunCallbacks{
		OnQueued: func() {
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
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
		OnTextDelta: func(text string) {
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
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
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
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
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
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

// CompactConversation queues a compaction request to the conversation's runner.
// Returns a RunHandle immediately.
func (self *Coordinator) CompactConversation(ctx context.Context, agentId, conversationId string) (*runners.CompactResult, error) {
	resultChan := make(chan messageResult, 1)
	self.enqueueMessage(agentId, conversationId, &queuedMessage{
		ctx:        ctx,
		agentId:    agentId,
		resultChan: resultChan,
		compact:    true,
	})

	select {
	case result := <-resultChan:
		return result.compactResult, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// AbortConversationRun cancels the current run and rejects all queued messages
// for the given conversation.
func (self *Coordinator) AbortConversationRun(conversationId string) bool {
	value, ok := self.activeRunners.Load(conversationId)
	if !ok {
		return false
	}
	conversationRunner := value.(*conversationRunner)
	conversationRunner.mutex.Lock()
	defer conversationRunner.mutex.Unlock()

	conversationRunner.aborted = true
	if conversationRunner.cancel != nil {
		conversationRunner.cancel()
	}

	// Drain queue with errors.
	for _, queued := range conversationRunner.queue {
		queued.resultChan <- messageResult{err: context.Canceled}
	}
	conversationRunner.queue = nil

	return true
}

// AbortRun looks up the conversation for a runner ID and aborts it.
func (self *Coordinator) AbortRun(runId string) bool {
	value, ok := self.activeRunIdConversationIds.Load(runId)
	if !ok {
		return false
	}
	conversationId := value.(string)
	return self.AbortConversationRun(conversationId)
}

// GetActiveConversationRunner returns the active runner for the conversation, or nil.
func (self *Coordinator) GetActiveConversationRunner(conversationId string) *runners.Runner {
	value, ok := self.activeRunners.Load(conversationId)
	if !ok {
		return nil
	}
	active := value.(*conversationRunner)
	active.mutex.Lock()
	runner := active.runner
	active.mutex.Unlock()
	return runner
}

// GetActiveConversationRunID returns the active runId for the conversation, or empty string.
func (self *Coordinator) GetActiveConversationRunID(conversationId string) string {
	value, ok := self.activeConversationIdRunIds.Load(conversationId)
	if !ok {
		return ""
	}
	return value.(string)
}

// dispatchResult holds the outcome of a dispatched message.
type dispatchResult struct {
	runResult *runners.RunResult
	err       error
	aborted   bool
}

// dispatchMessage routes a message to the per-conversation queue, blocking until processed.
func (self *Coordinator) dispatchMessage(ctx context.Context, agentId, conversationId string, parameters runners.RunParameters, callbacks *runners.RunCallbacks) dispatchResult {
	resultChan := make(chan messageResult, 1)
	queued := &queuedMessage{
		ctx:        ctx,
		agentId:    agentId,
		parameters: parameters,
		callbacks:  callbacks,
		resultChan: resultChan,
	}

	self.enqueueMessage(agentId, conversationId, queued)

	// Block until the runner finishes. processQueue uses the coordinator's
	// long-lived context, so the run continues even if the caller disconnects.
	// We must wait for the actual result rather than short-circuiting on
	// ctx.Done(), because premature return would trigger cleanup of the
	// activeRunId tracking while the tool is still executing.
	result := <-resultChan
	return dispatchResult{runResult: result.runResult, err: result.err, aborted: result.aborted}
}

func (self *Coordinator) enqueueMessage(agentId, conversationId string, message *queuedMessage) {
	value, _ := self.activeRunners.LoadOrStore(conversationId, &conversationRunner{})
	conversationRunnerInstance := value.(*conversationRunner)

	conversationRunnerInstance.mutex.Lock()

	if conversationRunnerInstance.aborted {
		conversationRunnerInstance.mutex.Unlock()
		message.resultChan <- messageResult{err: context.Canceled}
		return
	}

	conversationRunnerInstance.queue = append(conversationRunnerInstance.queue, message)

	if !conversationRunnerInstance.processing {
		conversationRunnerInstance.processing = true

		// It is ok to use the first message's ctx to create the runner
		// createRunner and runners.NewRunner do not actually save this ctx
		runner := self.createRunner(message.ctx, agentId, conversationId)
		conversationRunnerInstance.runner = runner

		conversationRunnerInstance.mutex.Unlock()
		go self.processQueue(conversationId, conversationRunnerInstance)
	} else {
		// Notify caller that the run is queued.
		if message.callbacks != nil && message.callbacks.OnQueued != nil {
			message.callbacks.OnQueued()
		}
		conversationRunnerInstance.mutex.Unlock()
	}
}

func (self *Coordinator) processQueue(conversationId string, conversationRunnerInstance *conversationRunner) {
	defer deferutil.Recover()

	for {
		conversationRunnerInstance.mutex.Lock()

		if len(conversationRunnerInstance.queue) == 0 {
			conversationRunnerInstance.processing = false
			conversationRunnerInstance.runner = nil
			self.activeRunners.Delete(conversationId)
			conversationRunnerInstance.mutex.Unlock()
			return
		}

		// Dequeue next message.
		message := conversationRunnerInstance.queue[0]
		conversationRunnerInstance.queue = conversationRunnerInstance.queue[1:]

		var ctx context.Context
		var cancel context.CancelFunc
		if message.compact {
			// For compact, just pass in the caller's
			ctx, cancel = context.WithCancel(message.ctx)
		} else {
			// Build a run context from the coordinator's long-lived context so the
			// runner keeps running even if the caller (e.g. WebSocket) disconnects.
			// Carry over the authenticated user from the caller's context.
			// Ensure coordinator is on the context.
			ctx, cancel = context.WithCancel(
				tab.ContextWithTabToolBroker(
					askuser.ContextWithQuestionBroker(
						runners.ContextWithOrigin(
							ContextWithCoordinator(pubsub.ContextWithPubSub(models.ContextWithUserSessionToken(
								self.ctx,
								models.UserFromContext(message.ctx),
								models.SessionFromContext(message.ctx),
								models.TokenFromContext(message.ctx),
							), self.pubsub), self),
							message.parameters.Origin,
						),
						self.questionBroker,
					),
					self.tabToolBroker,
				))
			conversationRunnerInstance.cancel = cancel
		}

		runner := conversationRunnerInstance.runner

		conversationRunnerInstance.mutex.Unlock()

		// Execute the message.
		var result messageResult
		if runner != nil {
			if message.compact {
				compactResult, err := runner.CompactConversation(ctx)
				result = messageResult{compactResult: compactResult, err: err}
			} else {
				runResult, err := runner.Run(ctx, message.parameters, message.callbacks)
				result = messageResult{runResult: runResult, err: err}
			}
		} else {
			result = messageResult{err: fmt.Errorf("cannot create runner")}
		}

		cancel()

		// Tag the result if this run was aborted.
		conversationRunnerInstance.mutex.Lock()
		if conversationRunnerInstance.aborted {
			result.aborted = true
		}
		conversationRunnerInstance.mutex.Unlock()

		message.resultChan <- result
	}
}

func (self *Coordinator) createRunner(ctx context.Context, agentId, conversationId string) *runners.Runner {
	// Query store for agent to build tools.
	var agent *models.Agent
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		agent, err = transaction.GetAgent(ctx, agentId, nil)
		return err
	}); err != nil {
		return nil
	}
	if agent == nil {
		return nil
	}

	return runners.NewRunner(ctx, agentId, conversationId, self.providerRegistry, *agent)
}

// lifecycle returns the lifecycle manager from the coordinator context.
func (self *Coordinator) lifecycle() lifecycle.Lifecycle {
	return lifecycle.LifecycleFromContext(self.ctx)
}
