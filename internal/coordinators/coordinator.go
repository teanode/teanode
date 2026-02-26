// Package coordinators implements runner lifecycle management and message routing.
package coordinators

import (
	"context"
	"fmt"
	"sync"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("coordinator")

// BuildToolRegistryFunc creates a tool registry and skill prompts for an agent.
type BuildToolRegistryFunc func(ctx context.Context, agent models.Agent) (*toolregistry.ToolRegistry, string)

// Coordinator owns runner creation, the active runner map, and message routing.
// It implements runners.RunCoordinator.
type Coordinator struct {
	providers         *providers.Registry
	buildToolRegistry BuildToolRegistryFunc
	activeRunners     sync.Map // conversationId → *conversationRunner
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
func New(providerRegistry *providers.Registry, buildToolRegistry BuildToolRegistryFunc) *Coordinator {
	return &Coordinator{
		providers:         providerRegistry,
		buildToolRegistry: buildToolRegistry,
	}
}

// Providers returns the provider registry.
func (self *Coordinator) Providers() *providers.Registry {
	return self.providers
}

// SendMessage routes a message to the appropriate runner, creating one if needed.
// It blocks until this message is processed.
func (self *Coordinator) SendMessage(ctx context.Context, agentId, conversationId string, params runners.RunParams, callbacks *runners.RunCallbacks) (*runners.RunResult, error) {
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

// CompactConversation queues a compaction request to the conversation's runner.
func (self *Coordinator) CompactConversation(ctx context.Context, agentId, conversationId string) (*runners.CompactResult, error) {
	resultChan := make(chan messageResult, 1)
	queued := &queuedMessage{
		ctx:        ctx,
		agentId:    agentId,
		resultChan: resultChan,
		compact:    true,
	}

	self.enqueueMessage(conversationId, agentId, queued)

	select {
	case result := <-resultChan:
		return result.compactResult, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// AbortConversation cancels the current run and rejects all queued messages
// for the given conversation.
func (self *Coordinator) AbortConversation(conversationId string) bool {
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

// GetActiveConversationRunner returns true if there is an active runner for the conversation.
func (self *Coordinator) GetActiveConversationRunner(conversationId string) bool {
	_, ok := self.activeRunners.Load(conversationId)
	return ok
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
		messageContext = runners.ContextWithCoordinator(messageContext, self)

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

	tools, skillPrompts := self.buildToolRegistry(ctx, *agent)
	return &runners.Runner{
		AgentID:        agentId,
		ConversationID: conversationId,
		Providers:      self.providers,
		Tools:          tools,
		SkillPrompts:   skillPrompts,
	}
}
