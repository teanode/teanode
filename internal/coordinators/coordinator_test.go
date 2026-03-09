package coordinators

import (
	"context"
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
)

type testProvider struct {
	providers.BaseProvider
}

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

// blockingStreamProvider blocks in ChatCompletionStream until unblock is closed.
// It signals on started when the stream begins.
type blockingStreamProvider struct {
	providers.BaseProvider
	started chan struct{}
	unblock chan struct{}
}

func (self *blockingStreamProvider) ChatCompletion(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}

func (self *blockingStreamProvider) ChatCompletionStream(_ context.Context, request providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	events := make(chan providers.StreamEvent)
	go func() {
		defer close(events)
		select {
		case self.started <- struct{}{}:
		default:
		}
		<-self.unblock
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

func (self *blockingStreamProvider) ListModels(_ context.Context) ([]providers.ModelInformation, error) {
	return []providers.ModelInformation{{ID: "test-model"}}, nil
}

func newTestCoordinatorWithProvider(t *testing.T, provider providers.ChatProvider) (*Coordinator, context.Context, string) {
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
	providerRegistry.Register("mock", provider)

	events := pubsub.New()
	contextWithStore = lifecycle.ContextWithLifecycle(contextWithStore, lifecycle.New())

	configuration := &models.Configuration{
		Models: &models.ModelsConfiguration{Default: ptrto.Value("mock:test-model")},
	}
	coordinator := New(contextWithStore, configuration, providerRegistry, nil, events)

	return coordinator, contextWithStore, agentId
}

func newTestCoordinator(t *testing.T) (*Coordinator, context.Context, string) {
	return newTestCoordinatorWithProvider(t, &testProvider{})
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
		Origin:         runners.OriginWeb,
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
		Origin:         runners.OriginWeb,
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
		Origin:         runners.OriginChannel,
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

// TestActiveRunIdPersistsDuringExecution verifies that
// GetActiveConversationRunID returns a non-empty value while the runner is
// executing, even after the caller's context is cancelled (simulating a
// WebSocket disconnect / page refresh).
func TestActiveRunIdPersistsDuringExecution(t *testing.T) {
	started := make(chan struct{}, 1)
	unblock := make(chan struct{})

	coordinator, contextWithStore, agentId := newTestCoordinatorWithProvider(t, &blockingStreamProvider{
		started: started,
		unblock: unblock,
	})
	userId := "user-1"

	// Use a cancellable context to simulate a WebSocket connection that
	// disconnects (page refresh) while the run is in progress.
	callerCtx, callerCancel := context.WithCancel(
		models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil),
	)
	defer callerCancel()

	handle, sendError := coordinator.Run(callerCtx, RunParameters{
		AgentID: agentId,
		Message: "hello",
		Origin:  runners.OriginWeb,
	}, nil)
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}

	// Wait for the provider to start streaming.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider to start")
	}

	// While the provider is blocked, activeRunId must be present.
	if got := coordinator.GetActiveConversationRunID(handle.ConversationID); got == "" {
		t.Fatal("activeRunId should be non-empty during execution")
	}

	// Cancel the caller context — simulates page refresh / WS disconnect.
	callerCancel()
	time.Sleep(100 * time.Millisecond)

	// activeRunId must STILL be present: processQueue uses the coordinator's
	// long-lived context, so the run continues independently of the caller.
	if got := coordinator.GetActiveConversationRunID(handle.ConversationID); got == "" {
		t.Fatal("activeRunId should survive caller context cancellation")
	}

	// Unblock the provider so the run can finish.
	close(unblock)

	// Wait for run to complete.
	select {
	case <-handle.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for run to complete")
	}

	// After completion, activeRunId must be cleaned up.
	// Allow a brief window for the cleanup goroutine to run.
	time.Sleep(100 * time.Millisecond)
	if got := coordinator.GetActiveConversationRunID(handle.ConversationID); got != "" {
		t.Fatalf("activeRunId should be empty after completion, got %q", got)
	}
}
