package telegram

import (
	"context"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
)

func TestShouldForwardDisconnectedWebUI(t *testing.T) {
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
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

	defaults := runners.NewDefaultConversationManager(contextWithStore)
	defaults.SetDefaultConversation("user-1", "main", "default-conversation")

	gateway := gw.New(
		contextWithStore,
		&models.Configuration{},
		coordinators.New(nil, nil),
		defaults,
		nil,
		nil,
		nil,
	)
	bot := &Bot{
		ctx:     contextWithStore,
		gateway: gateway,
	}

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

func TestUnlinkedTelegramMessage(t *testing.T) {
	message := unlinkedTelegramMessage("12345")
	for _, want := range []string{
		"not linked",
		"security.yaml",
		"channelLinks:",
		"telegram:",
		"\"12345\": \"<userId>\"",
		"users:",
	} {
		if !strings.Contains(strings.ToLower(message), strings.ToLower(want)) {
			t.Fatalf("message missing %q: %s", want, message)
		}
	}
}
