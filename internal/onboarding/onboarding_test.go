package onboarding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/prompts"
)

func newTestGateway(t *testing.T) gw.Gateway {
	t.Helper()
	configs.SetDirectory(t.TempDir())
	t.Cleanup(func() { configs.SetDirectory("") })

	registry := agents.NewAgentRegistry()
	registry.Register("main", &agents.Runner{AgentID: "main"})
	return gw.New(
		&configs.Config{
			AgentConfigs: []configs.AgentConfig{{ID: "main", Name: "Tea"}},
		},
		&configs.SecurityConfig{Users: map[string]configs.SecurityUser{}},
		registry,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func TestInitializeUserIsIdempotent(t *testing.T) {
	gateway := newTestGateway(t)
	userId := "user-1"
	agentId := "main"

	if err := InitializeUser(gateway, userId); err != nil {
		t.Fatalf("InitializeUser first call failed: %v", err)
	}
	firstDefaultConversationId := gateway.EnsureDefaultConversation(userId, agentId)
	if firstDefaultConversationId == "" {
		t.Fatal("expected default conversation id to be set")
	}

	if err := InitializeUser(gateway, userId); err != nil {
		t.Fatalf("InitializeUser second call failed: %v", err)
	}
	secondDefaultConversationId := gateway.EnsureDefaultConversation(userId, agentId)
	if secondDefaultConversationId != firstDefaultConversationId {
		t.Fatalf("default conversation changed across retries: %q -> %q", firstDefaultConversationId, secondDefaultConversationId)
	}

	store := gateway.ConversationStore(userId, agentId)
	conversationList, err := store.List()
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(conversationList) != 1 {
		t.Fatalf("expected exactly one onboarding conversation, got %d", len(conversationList))
	}

	messages, err := store.Load(firstDefaultConversationId)
	if err != nil {
		t.Fatalf("load seeded conversation: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected one seeded assistant message, got %d", len(messages))
	}
	if messages[0].Role != "assistant" {
		t.Fatalf("seeded message role = %q, want assistant", messages[0].Role)
	}
	if messages[0].ContentText() != prompts.OnboardingSeedMessage {
		t.Fatalf("seeded message content = %q, want %q", messages[0].ContentText(), prompts.OnboardingSeedMessage)
	}
	if len(messages[0].Metadata) != 0 {
		t.Fatal("seeded message should not include metadata marker")
	}
}

func TestInitializeUserDoesNotSeedWhenOnboardingMissing(t *testing.T) {
	gateway := newTestGateway(t)
	userId := "user-2"
	agentId := "main"

	if err := configs.EnsureUserDirectories(userId); err != nil {
		t.Fatalf("EnsureUserDirectories failed: %v", err)
	}
	workspaceDirectory := configs.UserWorkspaceDirectory(userId)
	if err := os.Remove(filepath.Join(workspaceDirectory, "ONBOARDING.md")); err != nil {
		t.Fatalf("remove ONBOARDING.md failed: %v", err)
	}

	if err := InitializeUser(gateway, userId); err != nil {
		t.Fatalf("InitializeUser failed: %v", err)
	}

	store := gateway.ConversationStore(userId, agentId)
	conversationList, err := store.List()
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(conversationList) != 0 {
		t.Fatalf("expected no seeded conversations when ONBOARDING.md is missing, got %d", len(conversationList))
	}
}

func TestInitializeUserDoesNotSeedWhenUserMessageExists(t *testing.T) {
	gateway := newTestGateway(t)
	userId := "user-3"
	agentId := "main"

	if err := configs.EnsureUserDirectories(userId); err != nil {
		t.Fatalf("EnsureUserDirectories failed: %v", err)
	}

	store := gateway.ConversationStore(userId, agentId)
	conversationId := "conversation-existing"
	if err := store.Append(conversationId, conversations.NewTextMessage("user", "Already started", 1)); err != nil {
		t.Fatalf("append existing user message: %v", err)
	}
	gateway.SetDefaultConversation(userId, agentId, conversationId)

	if err := InitializeUser(gateway, userId); err != nil {
		t.Fatalf("InitializeUser failed: %v", err)
	}

	conversationList, err := store.List()
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(conversationList) != 1 {
		t.Fatalf("expected one existing conversation only, got %d", len(conversationList))
	}

	messages, err := store.Load(conversationId)
	if err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected existing conversation to remain unchanged, got %d messages", len(messages))
	}
	if messages[0].Role != "user" {
		t.Fatalf("first message role = %q, want user", messages[0].Role)
	}
}
