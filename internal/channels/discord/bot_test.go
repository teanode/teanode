package discord

import (
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

func TestShouldForwardDisconnectedWebUI(t *testing.T) {
	t.Setenv("TEANODE_DIR", t.TempDir())

	registry := agents.NewAgentRegistry()
	registry.SetDefault(configs.DefaultAgentID)
	registry.SetDefaultConversation(configs.DefaultAgentID, "default-conversation")

	bot := &Bot{agentRegistry: registry}

	if !bot.shouldForwardDisconnectedWebUI(configs.DefaultAgentID, "default-conversation", "session-1") {
		t.Fatal("expected default agent/default conversation to be eligible for disconnected WebUI forwarding")
	}
	if bot.shouldForwardDisconnectedWebUI("other-agent", "default-conversation", "session-1") {
		t.Fatal("expected non-default agent to be rejected")
	}
	if bot.shouldForwardDisconnectedWebUI(configs.DefaultAgentID, "other-conversation", "session-1") {
		t.Fatal("expected non-default conversation to be rejected")
	}
	if bot.shouldForwardDisconnectedWebUI(configs.DefaultAgentID, "default-conversation", "") {
		t.Fatal("expected missing origin session to be rejected")
	}
}
