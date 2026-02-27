package discord

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
)

func TestShouldForwardDisconnectedWebUI(t *testing.T) {
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	// Seed a user with DefaultAgentID so shouldForwardDisconnectedSession can read it from the store.
	defaultAgentId := "main"
	_ = openedStore.Transaction(contextWithStore, func(ctx context.Context, transaction store.Transaction) error {
		_, err := transaction.CreateUser(ctx, &models.User{
			ID:             "user-1",
			DefaultAgentID: &defaultAgentId,
		}, nil, nil)
		return err
	})

	// Seed agent in store.
	_ = openedStore.Transaction(contextWithStore, func(ctx context.Context, transaction store.Transaction) error {
		_, _ = transaction.CreateAgent(ctx, &models.Agent{ID: "main"}, nil, nil)
		return nil
	})

	events := pubsub.New()
	coordinator := coordinators.New(contextWithStore, &models.Configuration{}, nil, nil, events)
	coordinator.SetDefaultConversation("user-1", "main", "default-conversation")

	bot := &Bot{ctx: contextWithStore, coordinator: coordinator}

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
