package gw

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/providers"
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
	t.Setenv("TEANODE_DIR", t.TempDir())

	agentId := configs.DefaultAgentID
	config := &configs.Config{
		Models: configs.ModelsConfig{
			Default: "mock:test-model",
		},
	}
	providerRegistry := providers.NewRegistry("mock")
	providerRegistry.Register("mock", &testProvider{})
	runner := &agents.Runner{
		AgentID:   agentId,
		Providers: providerRegistry,
		ResolveConversations: func(userId, agentId string) *conversations.Store {
			return conversations.NewStore(t.TempDir())
		},
		ResolveUserProfile: func(userId string) (*configs.UserProfile, error) {
			return &configs.UserProfile{Name: userId}, nil
		},
		Config: config,
	}

	agentRegistry := agents.NewAgentRegistry()
	agentRegistry.Register(agentId, runner)
	agentRegistry.SetDefault(agentId)

	instance := New(config, nil, agentRegistry, nil, nil, nil, nil, nil, nil)
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

	existingDefaultConversationId := gateway.NewConversation(userId, agentId, "")

	handle := gateway.SendMessage(context.Background(), SendMessageParameters{
		UserContext:    &UserContext{UserID: userId},
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	waitRun(t, handle)

	if handle.ConversationID == existingDefaultConversationId {
		t.Fatalf("auto-created conversation id = existing default %q; want new conversation id", existingDefaultConversationId)
	}
	if got := agentRegistry.DefaultConversationID(userId, agentId); got != existingDefaultConversationId {
		t.Fatalf("default conversation id = %q, want %q", got, existingDefaultConversationId)
	}
}

func TestSendMessageAutoCreateSetsDefaultWhenUnset(t *testing.T) {
	gateway, agentRegistry, agentId := newTestGateway(t)
	userId := "user-1"

	handle := gateway.SendMessage(context.Background(), SendMessageParameters{
		UserContext:    &UserContext{UserID: userId},
		AgentID:        agentId,
		ConversationID: "",
		Message:        "hello",
		Origin:         "webui",
	}, nil)
	waitRun(t, handle)

	if got := agentRegistry.DefaultConversationID(userId, agentId); got != handle.ConversationID {
		t.Fatalf("default conversation id = %q, want %q", got, handle.ConversationID)
	}
}

func TestNewConversationReplacesDefaultConversation(t *testing.T) {
	gateway, agentRegistry, agentId := newTestGateway(t)
	userId := "user-1"

	firstConversationId := gateway.NewConversation(userId, agentId, "")
	secondConversationId := gateway.NewConversation(userId, agentId, "")

	if secondConversationId == firstConversationId {
		t.Fatalf("second conversation id = first conversation id %q; want different id", firstConversationId)
	}
	if got := agentRegistry.DefaultConversationID(userId, agentId); got != secondConversationId {
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

	handle := gateway.SendMessage(context.Background(), SendMessageParameters{
		UserContext:    &UserContext{UserID: userId},
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
