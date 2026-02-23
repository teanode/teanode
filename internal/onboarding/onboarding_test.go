package onboarding

import (
	"encoding/json"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/gw"
)

func newTestGateway(t *testing.T) gw.Gateway {
	t.Helper()
	configs.SetDirectory(t.TempDir())
	t.Cleanup(func() { configs.SetDirectory("") })

	registry := agents.NewAgentRegistry()
	registry.SetDefault(configs.DefaultAgentID)
	return gw.New(
		&configs.Config{},
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
	agentId := configs.DefaultAgentID

	if err := InitializeUser(gateway, userId); err != nil {
		t.Fatalf("InitializeUser first call failed: %v", err)
	}
	firstDefaultConversationId := gateway.DefaultConversationID(userId, agentId)
	if firstDefaultConversationId == "" {
		t.Fatal("expected default conversation id to be set")
	}

	if err := InitializeUser(gateway, userId); err != nil {
		t.Fatalf("InitializeUser second call failed: %v", err)
	}
	secondDefaultConversationId := gateway.DefaultConversationID(userId, agentId)
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
	if len(messages[0].Metadata) == 0 {
		t.Fatal("seeded message is missing metadata marker")
	}
	var marker seedMarkerEnvelope
	if err := json.Unmarshal(messages[0].Metadata, &marker); err != nil {
		t.Fatalf("unmarshal marker: %v", err)
	}
	if marker.Teanode.OnboardingSeedVersion != SeedVersion {
		t.Fatalf("marker version = %d, want %d", marker.Teanode.OnboardingSeedVersion, SeedVersion)
	}
}
