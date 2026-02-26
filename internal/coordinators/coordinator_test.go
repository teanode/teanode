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
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	toolregistry "github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
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
				Model: request.Model,
				Choices: []providers.StreamChoice{
					{
						Delta:        providers.ChatDelta{Content: "ok"},
						FinishReason: "stop",
					},
				},
				Usage: &providers.UsageInfo{
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

func (self *testProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{{ID: "test-model"}}, nil
}

func newTestCoordinator(t *testing.T) (*Coordinator, *runners.DefaultConversationManager, context.Context, string) {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
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

	providerRegistry := providers.NewRegistry("mock")
	providerRegistry.Register("mock", &testProvider{})

	events := pubsub.New()
	defaults := runners.NewDefaultConversationManager(contextWithStore)
	contextWithStore = lifecycle.ContextWithLifecycle(contextWithStore, lifecycle.New())

	configuration := &models.Configuration{
		Models: &models.ModelsConfiguration{Default: ptrto.Value("mock:test-model")},
	}
	coordinator := New(contextWithStore, configuration, providerRegistry, defaults, nil, events, func(_ context.Context, _ models.Agent) (*toolregistry.ToolRegistry, string) {
		return toolregistry.NewToolRegistry(), ""
	})

	return coordinator, defaults, contextWithStore, agentId
}

func waitRunHandle(t *testing.T, handle *RunHandle) {
	t.Helper()
	result, err := handle.Wait()
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	_ = result
}

func TestSendMessageAutoCreateDoesNotOverwriteExistingDefaultConversation(t *testing.T) {
	coordinator, defaults, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	existingDefaultConversationId := coordinator.NewDefaultConversation(userId, agentId, "")

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	handle, sendError := coordinator.SendMessage(runContext, SendMessageParameters{
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
	if got := defaults.EnsureDefaultConversation(userId, agentId); got != existingDefaultConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, existingDefaultConversationId)
	}
}

func TestSendMessageAutoCreateSetsDefaultWhenUnset(t *testing.T) {
	coordinator, defaults, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	handle, sendError := coordinator.SendMessage(runContext, SendMessageParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	if sendError != nil {
		t.Fatalf("send error: %v", sendError)
	}
	waitRunHandle(t, handle)

	if got := defaults.EnsureDefaultConversation(userId, agentId); got != handle.ConversationID {
		t.Fatalf("default conversation id = %q, want %q", got, handle.ConversationID)
	}
}

func TestNewConversationReplacesDefaultConversation(t *testing.T) {
	coordinator, defaults, _, agentId := newTestCoordinator(t)
	userId := "user-1"

	firstConversationId := coordinator.NewDefaultConversation(userId, agentId, "")
	secondConversationId := coordinator.NewDefaultConversation(userId, agentId, "")

	if secondConversationId == firstConversationId {
		t.Fatalf("second conversation id = first conversation id %q; want different id", firstConversationId)
	}
	if got := defaults.EnsureDefaultConversation(userId, agentId); got != secondConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, secondConversationId)
	}
}

func TestDeferredLifecycleFiresAfterRunDone(t *testing.T) {
	coordinator, _, contextWithStore, agentId := newTestCoordinator(t)
	userId := "user-1"

	lifecycleManager := lifecycle.LifecycleFromContext(contextWithStore)
	lifecycleManager.ScheduleLifecycle(lifecycle.Restart)

	runContext := models.ContextWithUserSessionToken(contextWithStore, &models.User{ID: userId}, nil, nil)
	handle, sendError := coordinator.SendMessage(runContext, SendMessageParameters{
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
