package discord

import (
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

func TestShouldForwardDisconnectedWebUI(t *testing.T) {
	t.Setenv("TEANODE_DIR", t.TempDir())

	registry := agents.NewAgentRegistry()
	registry.SetDefault(configs.DefaultAgentID)
	registry.SetDefaultConversation("user-1", configs.DefaultAgentID, "default-conversation")

	bot := &Bot{agentRegistry: registry}

	if !bot.shouldForwardDisconnectedSession("user-1", configs.DefaultAgentID, "default-conversation", "session-1") {
		t.Fatal("expected default agent/default conversation to be eligible for disconnected WebUI forwarding")
	}
	if bot.shouldForwardDisconnectedSession("user-1", "other-agent", "default-conversation", "session-1") {
		t.Fatal("expected non-default agent to be rejected")
	}
	if bot.shouldForwardDisconnectedSession("user-1", configs.DefaultAgentID, "other-conversation", "session-1") {
		t.Fatal("expected non-default conversation to be rejected")
	}
	if bot.shouldForwardDisconnectedSession("user-1", configs.DefaultAgentID, "default-conversation", "") {
		t.Fatal("expected missing origin session to be rejected")
	}
}

func TestUnlinkedDiscordMessage(t *testing.T) {
	t.Setenv("TEANODE_DIR", t.TempDir())
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
