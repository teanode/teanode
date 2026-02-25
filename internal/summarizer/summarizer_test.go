package summarizer

import (
	"context"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
)

func TestSummarizerSummarizeAllIteratesAllUsersAndAgents(t *testing.T) {
	baseDirectory := t.TempDir()

	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: baseDirectory})
	if openError != nil {
		t.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrate store: %v", migrateError)
	}
	if createError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		for _, userID := range []string{"user-b", "user-a"} {
			username := userID
			admin := false
			if _, err := transaction.CreateUser(context.Background(), &models.User{
				ID:       userID,
				Username: &username,
				Admin:    &admin,
			}, nil, nil); err != nil {
				return err
			}
		}
		if _, err := transaction.CreateAgent(ctx, &models.Agent{ID: "agent-a"}, nil, nil); err != nil {
			return err
		}
		if _, err := transaction.CreateAgent(ctx, &models.Agent{ID: "agent-b"}, nil, nil); err != nil {
			return err
		}
		return nil
	}); createError != nil {
		t.Fatalf("create users/agents: %v", createError)
	}
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	instance := New(contextWithStore, nil)
	// summarizeAll should iterate all user-agent combinations without panicking.
	// No provider is configured, so summarization will be silently skipped,
	// but the iteration logic is exercised.
	instance.summarizeAll(contextWithStore)
}

func TestSummarizerSummarizeAllWithEmptyConversationStore(t *testing.T) {
	baseDirectory := t.TempDir()

	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: baseDirectory})
	if openError != nil {
		t.Fatalf("open store: %v", openError)
	}
	defer openedStore.Close()
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrate store: %v", migrateError)
	}
	if createError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		userID := "user-1"
		username := userID
		admin := false
		if _, err := transaction.CreateUser(context.Background(), &models.User{
			ID:       userID,
			Username: &username,
			Admin:    &admin,
		}, nil, nil); err != nil {
			return err
		}
		if _, err := transaction.CreateAgent(ctx, &models.Agent{ID: "agent-a"}, nil, nil); err != nil {
			return err
		}
		return nil
	}); createError != nil {
		t.Fatalf("create user/agent: %v", createError)
	}
	contextWithStore := store.ContextWithStore(context.Background(), openedStore)

	instance := New(contextWithStore, nil)
	instance.summarizeAll(contextWithStore)
}
