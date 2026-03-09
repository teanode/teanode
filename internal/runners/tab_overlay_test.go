package runners

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/integrations/tabs"
	"github.com/teanode/teanode/internal/models"
)

func TestBuildTabOverlay_NoBroker(t *testing.T) {
	ctx := context.Background()
	result := buildTabOverlay(ctx, "agent1", "conv1")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestBuildTabOverlay_NoUser(t *testing.T) {
	broker := tabs.NewTabBroker()
	ctx := tabs.ContextWithTabBroker(context.Background(), broker)
	result := buildTabOverlay(ctx, "agent1", "conv1")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestBuildTabOverlay_NoAttachment(t *testing.T) {
	broker := tabs.NewTabBroker()
	ctx := tabs.ContextWithTabBroker(context.Background(), broker)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user1"}, nil, nil)
	result := buildTabOverlay(ctx, "agent1", "conv1")
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestBuildTabOverlay_WithAttachment(t *testing.T) {
	broker := tabs.NewTabBroker()
	broker.Attach(tabs.Attachment{
		UserID:         "user1",
		AgentID:        "agent1",
		ConversationID: "conv1",
		TabURL:         "https://example.com/page",
		TabTitle:       "Example Page",
		TabID:          42,
	}, "conn1")

	ctx := tabs.ContextWithTabBroker(context.Background(), broker)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user1"}, nil, nil)

	result := buildTabOverlay(ctx, "agent1", "conv1")

	if !strings.Contains(result, "<attached-tab>") {
		t.Error("missing <attached-tab> tag")
	}
	if !strings.Contains(result, "Example Page") {
		t.Error("missing tab title")
	}
	if !strings.Contains(result, "https://example.com/page") {
		t.Error("missing tab URL")
	}
	if !strings.Contains(result, "</attached-tab>") {
		t.Error("missing closing tag")
	}
}

func TestBuildTabOverlay_DifferentConversation(t *testing.T) {
	broker := tabs.NewTabBroker()
	broker.Attach(tabs.Attachment{
		UserID:         "user1",
		AgentID:        "agent1",
		ConversationID: "conv-other",
		TabURL:         "https://example.com",
		TabTitle:       "Other",
		TabID:          1,
	}, "conn1")

	ctx := tabs.ContextWithTabBroker(context.Background(), broker)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user1"}, nil, nil)

	result := buildTabOverlay(ctx, "agent1", "conv1")
	if result != "" {
		t.Fatalf("expected empty for different conversation, got %q", result)
	}
}
