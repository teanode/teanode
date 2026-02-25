package gw

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
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

func newTestGateway(t *testing.T) (*gateway, *agents.AgentRegistry, string) {
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
	defaultModel := "mock:test-model"
	config := &models.Configuration{
		Models: &models.ModelsConfiguration{
			Default: &defaultModel,
		},
	}
	providerRegistry := providers.NewRegistry("mock")
	providerRegistry.Register("mock", &testProvider{})
	runner := &agents.Runner{
		AgentID:   agentId,
		Providers: providerRegistry,
		ResolveUser: func(userId string) (*models.User, error) {
			return &models.User{ID: userId, Username: &userId}, nil
		},
		Config: config,
	}

	agentRegistry := agents.NewAgentRegistry(contextWithStore)
	agentRegistry.Register(agentId, runner)

	instance := New(contextWithStore, config, agentRegistry, nil, nil, nil)
	return instance.(*gateway), agentRegistry, agentId
}

func waitRun(t *testing.T, handle *RunHandle) {
	t.Helper()
	<-handle.Done
	if outcome := handle.Outcome(); outcome.Error != nil {
		t.Fatalf("run error: %v", outcome.Error)
	}
}

func TestSendMessageAutoCreateDoesNotOverwriteExistingDefaultConversation(t *testing.T) {
	gateway, agentRegistry, agentId := newTestGateway(t)
	userId := "user-1"

	existingDefaultConversationId := gateway.NewDefaultConversation(userId, agentId, "")

	runContext := models.ContextWithUserSessionToken(gateway.ctx, &models.User{ID: userId}, nil, nil)
	handle := gateway.SendMessage(runContext, SendMessageParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	waitRun(t, handle)

	if handle.ConversationID == existingDefaultConversationId {
		t.Fatalf("auto-created conversation id = existing default %q; want new conversation id", existingDefaultConversationId)
	}
	if got := agentRegistry.EnsureDefaultConversation(userId, agentId); got != existingDefaultConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, existingDefaultConversationId)
	}
}

func TestSendMessageAutoCreateSetsDefaultWhenUnset(t *testing.T) {
	gateway, agentRegistry, agentId := newTestGateway(t)
	userId := "user-1"

	runContext := models.ContextWithUserSessionToken(gateway.ctx, &models.User{ID: userId}, nil, nil)
	handle := gateway.SendMessage(runContext, SendMessageParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	waitRun(t, handle)

	if got := agentRegistry.EnsureDefaultConversation(userId, agentId); got != handle.ConversationID {
		t.Fatalf("default conversation id = %q, want %q", got, handle.ConversationID)
	}
}

func TestNewConversationReplacesDefaultConversation(t *testing.T) {
	gateway, agentRegistry, agentId := newTestGateway(t)
	userId := "user-1"

	firstConversationId := gateway.NewDefaultConversation(userId, agentId, "")
	secondConversationId := gateway.NewDefaultConversation(userId, agentId, "")

	if secondConversationId == firstConversationId {
		t.Fatalf("second conversation id = first conversation id %q; want different id", firstConversationId)
	}
	if got := agentRegistry.EnsureDefaultConversation(userId, agentId); got != secondConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, secondConversationId)
	}
}

func TestDeferredLifecycleFiresAfterRunDone(t *testing.T) {
	gateway, _, agentId := newTestGateway(t)
	userId := "user-1"

	// Use an unbuffered channel so we can observe run completion state at the
	// exact instant the deferred lifecycle action is delivered.
	gateway.lifecycleChannel = make(chan LifecycleAction)
	gateway.ScheduleLifecycle(LifecycleRestart)

	runContext := models.ContextWithUserSessionToken(gateway.ctx, &models.User{ID: userId}, nil, nil)
	handle := gateway.SendMessage(runContext, SendMessageParameters{
		AgentID:        agentId,
		ConversationID: "",
		Message:        "restart after response",
		Origin:         "telegram",
	}, nil)

	doneClosedAtLifecycle := make(chan bool, 1)
	go func() {
		<-gateway.LifecycleChannel()
		select {
		case <-handle.Done:
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
	<-handle.Done
	if outcome := handle.Outcome(); outcome.Error != nil {
		t.Fatalf("run error: %v", outcome.Error)
	}
}
