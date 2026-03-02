package coordinators

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/lifecycle"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"

	// Register the datetime tool so toolCallingProvider tests can resolve it.
	_ "github.com/teanode/teanode/internal/tools/datetime"
)

type testProvider struct{}

func (self *testProvider) ChatCompletion(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (self *testProvider) ChatCompletionStream(_ context.Context, request providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	events := make(chan providers.StreamEvent, 2)
	go func() {
		defer close(events)
		events <- providers.StreamEvent{
			Chunk: &providers.StreamChunk{
				ModelName: request.ModelName,
				Choices: []providers.StreamChoice{
					{
						Delta:        providers.ChatDelta{Content: "ok"},
						FinishReason: "stop",
					},
				},
				Usage: &providers.UsageInformation{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			},
		}
		events <- providers.StreamEvent{Done: true}
	}()
	return events, nil
}

func (self *testProvider) ListModels(_ context.Context) ([]providers.ModelInformation, error) {
	return []providers.ModelInformation{{ID: "test-model"}}, nil
}

func newTestCoordinator(t *testing.T) (*Coordinator, context.Context, string) {
	t.Helper()
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	agentId := "main"

	// Seed agent and configuration in store.
	_ = openedStore.Transaction(contextWithStore, func(ctx context.Context, transaction store.Transaction) error {
		_, _ = transaction.CreateAgent(ctx, &models.Agent{ID: agentId}, nil, nil)
		_, _ = transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Models = &models.ModelsConfiguration{Default: ptrto.Value("mock:test-model")}
			return nil
		}, nil)
		return nil
	})

	providerRegistry := providers.NewProviderRegistry(&models.ModelsConfiguration{
		Default: ptrto.Value("mock:test-model"),
		Providers: &[]*models.ProviderConfiguration{
			{Name: ptrto.Value("mock"), APIKey: ptrto.Value("test")},
		},
	})
	providerRegistry.Register("mock", &testProvider{})

	events := pubsub.New()
	contextWithStore = lifecycle.ContextWithLifecycle(contextWithStore, lifecycle.New())

	configuration := &models.Configuration{
		Models: &models.ModelsConfiguration{Default: ptrto.Value("mock:test-model")},
	}
	coordinator := New(contextWithStore, configuration, providerRegistry, nil, events)

	return coordinator, contextWithStore, agentId
}

func waitRunHandle(t *testing.T, handle *RunHandle) {
	t.Helper()
	_, err := handle.Wait()
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
}

func TestRunAutoCreateDoesNotOverwriteExistingDefaultConversation(t *testing.T) {
	coordinator, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	existingDefaultConversationId := coordinator.NewDefaultConversation(userId, agentId)

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	handle, sendError := coordinator.Run(runContext, RunParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}
	waitRunHandle(t, handle)

	if handle.ConversationID == existingDefaultConversationId {
		t.Fatalf("auto-created conversation id = existing default %q; want new conversation id", existingDefaultConversationId)
	}
	if got := coordinator.EnsureDefaultConversation(userId, agentId); got != existingDefaultConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, existingDefaultConversationId)
	}
}

func TestRunAutoCreateSetsDefaultWhenUnset(t *testing.T) {
	coordinator, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	handle, sendError := coordinator.Run(runContext, RunParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}
	waitRunHandle(t, handle)

	if got := coordinator.EnsureDefaultConversation(userId, agentId); got != handle.ConversationID {
		t.Fatalf("default conversation id = %q, want %q", got, handle.ConversationID)
	}
}

func TestNewConversationReplacesDefaultConversation(t *testing.T) {
	coordinator, _, agentId := newTestCoordinator(t)
	userId := "user-1"

	firstConversationId := coordinator.NewDefaultConversation(userId, agentId)
	secondConversationId := coordinator.NewDefaultConversation(userId, agentId)

	if secondConversationId == firstConversationId {
		t.Fatalf("second conversation id = first conversation id %q; want different id", firstConversationId)
	}
	if got := coordinator.EnsureDefaultConversation(userId, agentId); got != secondConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, secondConversationId)
	}
}

func TestActiveRunStateThinkingDuringRun(t *testing.T) {
	coordinator, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	// Replace mock provider with a blocking one so we can inspect state mid-run.
	gate := make(chan struct{})
	coordinator.providerRegistry.Register("mock", &blockingProvider{gate: gate})

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	conversationId := coordinator.NewDefaultConversation(userId, agentId)

	handle, sendError := coordinator.Run(runContext, RunParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}

	// Wait for the run to actually start streaming (provider blocks on gate).
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider to be called")
	case <-gate: // provider signals it was called
	}

	// While the run is in-flight, state should be "thinking".
	state := coordinator.GetActiveRunState(conversationId)
	if state == nil {
		t.Fatal("expected non-nil active run state during run")
	}
	if state.Phase != "thinking" {
		t.Fatalf("phase = %q, want %q", state.Phase, "thinking")
	}

	// Unblock the provider.
	gate <- struct{}{}

	waitRunHandle(t, handle)

	// After completion, state should be cleaned up.
	if got := coordinator.GetActiveRunState(conversationId); got != nil {
		t.Fatalf("expected nil active run state after run, got %+v", got)
	}
}

// blockingProvider blocks on a gate channel during streaming.
// It sends on gate when called (so the test knows the run started),
// then waits on gate again before returning results.
type blockingProvider struct {
	gate chan struct{}
}

func (self *blockingProvider) ChatCompletion(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (self *blockingProvider) ChatCompletionStream(_ context.Context, request providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	events := make(chan providers.StreamEvent, 2)
	go func() {
		defer close(events)
		// Signal that the provider was called.
		self.gate <- struct{}{}
		// Wait for test to unblock.
		<-self.gate
		events <- providers.StreamEvent{
			Chunk: &providers.StreamChunk{
				ModelName: request.ModelName,
				Choices: []providers.StreamChoice{
					{
						Delta:        providers.ChatDelta{Content: "ok"},
						FinishReason: "stop",
					},
				},
				Usage: &providers.UsageInformation{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			},
		}
		events <- providers.StreamEvent{Done: true}
	}()
	return events, nil
}

func (self *blockingProvider) ListModels(_ context.Context) ([]providers.ModelInformation, error) {
	return []providers.ModelInformation{{ID: "test-model"}}, nil
}

// toolCallingProvider emits a tool call for the "datetime" builtin tool on the
// first stream, then responds with plain text on the second stream (after tool
// results have been appended to the conversation).
type toolCallingProvider struct {
	calls atomic.Int32
}

func (self *toolCallingProvider) ChatCompletion(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (self *toolCallingProvider) ChatCompletionStream(_ context.Context, request providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	call := self.calls.Add(1)
	events := make(chan providers.StreamEvent, 3)
	go func() {
		defer close(events)
		if call == 1 {
			// First call: emit a tool call for "datetime".
			events <- providers.StreamEvent{
				Chunk: &providers.StreamChunk{
					ModelName: request.ModelName,
					Choices: []providers.StreamChoice{
						{
							Delta: providers.ChatDelta{
								ToolCalls: []providers.ToolCallDelta{
									{
										Index: 0,
										ID:    "call-1",
										Type:  "function",
										Function: providers.FunctionCallDelta{
											Name:      "datetime",
											Arguments: "{}",
										},
									},
								},
							},
							FinishReason: "tool_calls",
						},
					},
					Usage: &providers.UsageInformation{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
				},
			}
		} else {
			// Second call (after tool result): return final text.
			events <- providers.StreamEvent{
				Chunk: &providers.StreamChunk{
					ModelName: request.ModelName,
					Choices: []providers.StreamChoice{
						{
							Delta:        providers.ChatDelta{Content: "done"},
							FinishReason: "stop",
						},
					},
					Usage: &providers.UsageInformation{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
				},
			}
		}
		events <- providers.StreamEvent{Done: true}
	}()
	return events, nil
}

func (self *toolCallingProvider) ListModels(_ context.Context) ([]providers.ModelInformation, error) {
	return []providers.ModelInformation{{ID: "test-model"}}, nil
}

func TestActiveRunStateToolPhaseViaCallbacks(t *testing.T) {
	coordinator, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	// We use caller callbacks to observe the run state at each transition.
	// The merged callbacks update the state *before* calling caller callbacks,
	// so by the time our callback runs the state reflects the new phase.
	var stateAtToolCall, stateAtToolResult *ActiveRunState

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	conversationId := coordinator.NewDefaultConversation(userId, agentId)

	// Replace mock provider with a tool-calling one.
	coordinator.providerRegistry.Register("mock", &toolCallingProvider{})

	handle, sendError := coordinator.Run(runContext, RunParameters{
		AgentID:        agentId,
		ConversationID: conversationId,
		Message:        "hello",
		Origin:         "webui",
	}, &runners.RunCallbacks{
		OnToolCall: func(toolName string, arguments string) {
			stateAtToolCall = coordinator.GetActiveRunState(conversationId)
		},
		OnToolResult: func(toolName string, result string) {
			stateAtToolResult = coordinator.GetActiveRunState(conversationId)
		},
	})
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}
	waitRunHandle(t, handle)

	// During tool call, state should have been "tool" with the tool name.
	if stateAtToolCall == nil {
		t.Fatal("expected non-nil state at tool call")
	}
	if stateAtToolCall.Phase != "tool" {
		t.Fatalf("tool call phase = %q, want %q", stateAtToolCall.Phase, "tool")
	}
	if stateAtToolCall.ToolName != "datetime" {
		t.Fatalf("tool call toolName = %q, want %q", stateAtToolCall.ToolName, "datetime")
	}

	// After tool result, state should revert to "thinking".
	if stateAtToolResult == nil {
		t.Fatal("expected non-nil state at tool result")
	}
	if stateAtToolResult.Phase != "thinking" {
		t.Fatalf("tool result phase = %q, want %q", stateAtToolResult.Phase, "thinking")
	}
}

func TestDeferredLifecycleFiresAfterRunDone(t *testing.T) {
	coordinator, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	lifecycleManager := lifecycle.LifecycleFromContext(contextWithStore)
	lifecycleManager.ScheduleLifecycle(lifecycle.Restart)

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	handle, sendError := coordinator.Run(runContext, RunParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "restart after response",
		Origin:         "telegram",
	}, nil)
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}

	doneClosedAtLifecycle := make(chan bool, 1)
	go func() {
		<-lifecycleManager.Channel()
		select {
		case <-handle.Done():
			doneClosedAtLifecycle <- true
		default:
			doneClosedAtLifecycle <- false
		}
	}()

	select {
	case closed := <-doneClosedAtLifecycle:
		if !closed {
			t.Fatalf("run done channel was not closed before deferred lifecycle fired")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for deferred lifecycle action")
	}

	// Ensure run completed successfully.
	_, waitError := handle.Wait()
	if waitError != nil {
		t.Fatalf("run error: %v", waitError)
	}
}
