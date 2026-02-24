package discord

import (
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/gw"
)

func TestShouldForwardDisconnectedWebUI(t *testing.T) {
	configs.SetDirectory(t.TempDir())

	registry := agents.NewAgentRegistry()
	registry.Register("main", &agents.Runner{AgentID: "main"})
	registry.SetDefaultConversation("user-1", "main", "default-conversation")

	gateway := gw.New(
		&configs.Config{AgentConfigs: []configs.AgentConfig{{ID: "main"}}},
		&configs.SecurityConfig{Users: map[string]configs.SecurityUser{}},
		registry,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	bot := &Bot{agentRegistry: registry, gateway: gateway}

	if !bot.shouldForwardDisconnectedSession("user-1", "main", "default-conversation", "session-1") {
		t.Fatal("expected default agent/default conversation to be eligible for disconnected WebUI forwarding")
	}
	if bot.shouldForwardDisconnectedSession("user-1", "other-agent", "default-conversation", "session-1") {
		t.Fatal("expected non-default agent to be rejected")
	}
	if bot.shouldForwardDisconnectedSession("user-1", "main", "other-conversation", "session-1") {
		t.Fatal("expected non-default conversation to be rejected")
	}
	if bot.shouldForwardDisconnectedSession("user-1", "main", "default-conversation", "") {
		t.Fatal("expected missing origin session to be rejected")
	}
}

func TestUnlinkedDiscordMessage(t *testing.T) {
	configs.SetDirectory(t.TempDir())
	message := unlinkedDiscordMessage("98765")
	for _, want := range []string{
		"not linked",
		"security.yaml",
		"channelLinks:",
		"discord:",
		"\"98765\": \"<userId>\"",
		"users:",
	} {
		if !strings.Contains(strings.ToLower(message), strings.ToLower(want)) {
			t.Fatalf("message missing %q: %s", want, message)
		}
	}
}
