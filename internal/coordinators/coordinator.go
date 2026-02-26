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
	"github.com/teanode/teanode/internal/summarizer"
	toolregistry "github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/security"
)

var log = logging.MustGetLogger("coordinator")

// BuildToolRegistryFunc creates a tool registry and skill prompts for an agent.
type BuildToolRegistryFunc func(ctx context.Context, agent models.Agent) (*toolregistry.ToolRegistry, string)

// Coordinator owns runner creation, the active runner map, message routing,
// default conversation management, and event broadcasting via pubsub.
// It implements RunCoordinator.
type Coordinator struct {
	ctx               context.Context
	config            *models.Configuration
	providerRegistry  *providers.ProviderRegistry
	summarizer        *summarizer.Summarizer
	pubsub            *pubsub.PubSub
	buildToolRegistry BuildToolRegistryFunc
	activeRunners     sync.Map // conversationId -> *conversationRunner
	runnerIndex       sync.Map // runnerId -> conversationId
}

type conversationRunner struct {
	mutex         sync.Mutex
	runner        *runners.Runner
	queue         []*queuedMessage
	processing    bool
	cancelCurrent context.CancelFunc
	aborted       bool
}

type queuedMessage struct {
	ctx        context.Context
	agentId    string
	params     runners.RunParams
	callbacks  *runners.RunCallbacks
	resultChan chan<- messageResult
	compact    bool // true for compact operations
}

type messageResult struct {
	runResult     *runners.RunResult
	compactResult *runners.CompactResult
	err           error
}

// New creates a Coordinator.
func New(
	ctx context.Context,
	config *models.Configuration,
	providerRegistry *providers.ProviderRegistry,
	summarizerInstance *summarizer.Summarizer,
	events *pubsub.PubSub,
	buildToolRegistry BuildToolRegistryFunc,
) *Coordinator {
	return &Coordinator{
		ctx:               ctx,
		config:            config,
		providerRegistry:  providerRegistry,
		summarizer:        summarizerInstance,
		pubsub:            events,
		buildToolRegistry: buildToolRegistry,
	}
}

// ProviderRegistry returns the provider registry.
func (self *Coordinator) ProviderRegistry() *providers.ProviderRegistry {
	return self.providerRegistry
}

// SendMessage orchestrates an agent run: resolves conversation, generates
// a runner ID, tracks the run, broadcasts all events, merges caller callbacks, and cleans
// up on completion. Returns a RunHandle immediately so the caller can wait or proceed.
func (self *Coordinator) SendMessage(ctx context.Context, parameters SendMessageParameters, callerCallbacks *runners.RunCallbacks) (*RunHandle, error) {
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
	runnerId := security.NewULID()
	runContext, cancel := context.WithCancel(ctx)

	// Track runner in index.
	self.runnerIndex.Store(runnerId, conversationId)

	// Create handle.
	handle := NewRunHandle(runnerId, conversationId)

	// Broadcast user message and conversations list update.
	userMessagePayload := map[string]interface{}{
		"state":          "user_message",
		"runnerId":       runnerId,
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
	mergedCallbacks := self.buildMergedCallbacks(runnerId, conversationId, resolvedAgentId, userId, callerCallbacks)

	// Ensure coordinator is on context.
	runContext = ContextWithCoordinator(runContext, self)

	// Run agent in background goroutine via internal dispatch.
	go func() {
		defer deferutil.Recover()
		defer func() {
			// Clean up runner index.
			self.runnerIndex.Delete(runnerId)
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

		result, err := self.dispatchMessage(runContext, resolvedAgentId, conversationId, runners.RunParams{
			Message:            parameters.Message,
			Model:              parameters.Model,
			Attachments:        parameters.Attachments,
			SystemPromptSuffix: parameters.SystemPromptSuffix,
			SystemPromptMode:   parameters.SystemPromptMode,
		}, mergedCallbacks)

		if err != nil {
			if runContext.Err() != nil {
				log.Warningf("run aborted runner=%s conversation=%s: %v", runnerId, conversationId, err)
				self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
					"state":          "aborted",
					"runnerId":       runnerId,
					"conversationId": conversationId,
					"agentId":        resolvedAgentId,
					"userId":         userId,
				})
			} else {
				log.Errorf("run error runner=%s conversation=%s: %v", runnerId, conversationId, err)
				self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
					"state":          "error",
					"runnerId":       runnerId,
					"conversationId": conversationId,
					"agentId":        resolvedAgentId,
					"userId":         userId,
					"error":          err.Error(),
				})
			}
			handle.Resolve(nil, nil, err)
			return
		}

		payload := map[string]interface{}{
			"state":          "final",
			"runnerId":       runnerId,
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
		self.pubsub.Broadcast(pubsub.EventTypeConversation, payload)
		handle.Resolve(result, nil, nil)
	}()

	return handle, nil
}

// buildMergedCallbacks creates RunCallbacks that both broadcast events and call the caller's optional callbacks.
func (self *Coordinator) buildMergedCallbacks(runnerId, conversationId, agentId, userId string, callerCallbacks *runners.RunCallbacks) *runners.RunCallbacks {
	var notifyOnce sync.Once

	return &runners.RunCallbacks{
		OnQueued: func() {
			self.pubsub.Broadcast(pubsub.EventTypeConversation, map[string]interface{}{
				"state":          "queued",
				"runnerId":       runnerId,
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
				"runnerId":       runnerId,
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
				"runnerId":       runnerId,
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
				"runnerId":       runnerId,
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
func (self *Coordinator) CompactConversation(ctx context.Context, agentId, conversationId string) (*RunHandle, error) {
	handle := NewRunHandle("", conversationId)
	resultChan := make(chan messageResult, 1)
	queued := &queuedMessage{
		ctx:        ctx,
		agentId:    agentId,
		resultChan: resultChan,
		compact:    true,
	}

	self.enqueueMessage(conversationId, agentId, queued)

	go func() {
		defer deferutil.Recover()
		select {
		case result := <-resultChan:
			handle.Resolve(nil, result.compactResult, result.err)
		case <-ctx.Done():
			handle.Resolve(nil, nil, ctx.Err())
		}
	}()

	return handle, nil
}

// AbortConversationRunner cancels the current run and rejects all queued messages
// for the given conversation.
func (self *Coordinator) AbortConversationRunner(conversationId string) bool {
	value, ok := self.activeRunners.Load(conversationId)
	if !ok {
		return false
	}
	conversationRunner := value.(*conversationRunner)
	conversationRunner.mutex.Lock()
	defer conversationRunner.mutex.Unlock()

	conversationRunner.aborted = true
	if conversationRunner.cancelCurrent != nil {
		conversationRunner.cancelCurrent()
	}

	// Drain queue with errors.
	for _, queued := range conversationRunner.queue {
		queued.resultChan <- messageResult{err: context.Canceled}
	}
	conversationRunner.queue = nil

	return true
}

// AbortRunner looks up the conversation for a runner ID and aborts it.
func (self *Coordinator) AbortRunner(runnerId string) bool {
	value, ok := self.runnerIndex.Load(runnerId)
	if !ok {
		return false
	}
	conversationId := value.(string)
	return self.AbortConversationRunner(conversationId)
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

// dispatchMessage routes a message to the per-conversation queue, blocking until processed.
func (self *Coordinator) dispatchMessage(ctx context.Context, agentId, conversationId string, params runners.RunParams, callbacks *runners.RunCallbacks) (*runners.RunResult, error) {
	resultChan := make(chan messageResult, 1)
	queued := &queuedMessage{
		ctx:        ctx,
		agentId:    agentId,
		params:     params,
		callbacks:  callbacks,
		resultChan: resultChan,
	}

	self.enqueueMessage(conversationId, agentId, queued)

	// Block until result.
	select {
	case result := <-resultChan:
		return result.runResult, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (self *Coordinator) enqueueMessage(conversationId, agentId string, message *queuedMessage) {
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

		// Create runner for this conversation.
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
			conversationRunnerInstance.mutex.Unlock()
			self.activeRunners.Delete(conversationId)
			return
		}

		// Dequeue next message.
		message := conversationRunnerInstance.queue[0]
		conversationRunnerInstance.queue = conversationRunnerInstance.queue[1:]

		// Create a cancellable context for this message.
		messageContext, cancel := context.WithCancel(message.ctx)
		conversationRunnerInstance.cancelCurrent = cancel

		// Ensure coordinator is on the context.
		messageContext = ContextWithCoordinator(messageContext, self)

		runner := conversationRunnerInstance.runner

		conversationRunnerInstance.mutex.Unlock()

		// Execute the message.
		var result messageResult
		if runner != nil {
			if message.compact {
				compactResult, err := runner.CompactConversation(messageContext)
				result = messageResult{compactResult: compactResult, err: err}
			} else {
				runResult, err := runner.Run(messageContext, message.params, message.callbacks)
				result = messageResult{runResult: runResult, err: err}
			}
		} else {
			result = messageResult{err: fmt.Errorf("cannot create runner")}
		}

		cancel()
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

	toolRegistry, skillPrompts := self.buildToolRegistry(ctx, *agent)
	return runners.NewRunner(agentId, conversationId, self.providerRegistry, toolRegistry, skillPrompts)
}

// lifecycle returns the lifecycle manager from the coordinator context.
func (self *Coordinator) lifecycle() lifecycle.Lifecycle {
	return lifecycle.LifecycleFromContext(self.ctx)
}
