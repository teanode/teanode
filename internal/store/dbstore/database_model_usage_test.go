package dbstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/security"
)

func TestCreateModelUsageEventDedup(t *testing.T) {
	s := openDatabaseStore(t)

	messageID := security.NewULID()
	event := &models.ModelUsageEvent{
		ID:               security.NewULID(),
		UserID:           security.NewULID(),
		ConversationID:   security.NewULID(),
		MessageID:        messageID,
		RunID:            security.NewULID(),
		ProviderName:     "anthropic",
		ModelName:        "claude-sonnet-4-5-20250514",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CreatedAt:        time.Now(),
	}

	// First insert should succeed.
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.CreateModelUsageEvent(ctx, event, nil)
	})
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Duplicate insert (same message_id) should succeed (ON CONFLICT DO NOTHING).
	dupEvent := *event
	dupEvent.ID = security.NewULID()
	dupEvent.PromptTokens = 999 // different values
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.CreateModelUsageEvent(ctx, &dupEvent, nil)
	})
	if err != nil {
		t.Fatalf("duplicate insert should not error: %v", err)
	}
}

func TestUpsertModelUsageStatEntryAdditivity(t *testing.T) {
	s := openDatabaseStore(t)

	userID := security.NewULID()
	startedAt := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	event1 := &models.ModelUsageEvent{
		ID:               security.NewULID(),
		UserID:           userID,
		ConversationID:   security.NewULID(),
		MessageID:        security.NewULID(),
		RunID:            security.NewULID(),
		ProviderName:     "anthropic",
		ModelName:        "claude-sonnet-4-5-20250514",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CreatedAt:        time.Now(),
	}

	event2 := &models.ModelUsageEvent{
		ID:               security.NewULID(),
		UserID:           userID,
		ConversationID:   security.NewULID(),
		MessageID:        security.NewULID(),
		RunID:            security.NewULID(),
		ProviderName:     "anthropic",
		ModelName:        "claude-sonnet-4-5-20250514",
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
		CreatedAt:        time.Now(),
	}

	// Upsert first event.
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.UpsertModelUsageStatEntry(ctx, event1, models.IntervalHourly, startedAt, nil)
	})
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Upsert second event (should add to existing).
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.UpsertModelUsageStatEntry(ctx, event2, models.IntervalHourly, startedAt, nil)
	})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Query and verify additivity.
	var entries []*models.ModelUsageStatEntry
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryModelUsageStatEntries(ctx, store.ModelUsageStatQuery{
			UserID:       userID,
			IntervalType: models.IntervalHourly,
			StartedAt:    startedAt,
			EndedAt:      startedAt.Add(time.Hour),
		}, nil)
		if err != nil {
			return err
		}
		entries = result
		return nil
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.PromptTokens != 300 {
		t.Errorf("prompt tokens: got %d, want 300", entry.PromptTokens)
	}
	if entry.CompletionTokens != 150 {
		t.Errorf("completion tokens: got %d, want 150", entry.CompletionTokens)
	}
	if entry.TotalTokens != 450 {
		t.Errorf("total tokens: got %d, want 450", entry.TotalTokens)
	}
	if entry.RequestCount != 2 {
		t.Errorf("request count: got %d, want 2", entry.RequestCount)
	}
}

func TestQueryModelUsageStatEntriesFilters(t *testing.T) {
	s := openDatabaseStore(t)

	userID := security.NewULID()
	startedAt := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)

	// Insert two different models.
	for _, modelName := range []string{"claude-sonnet-4-5-20250514", "gpt-4o"} {
		providerName := "anthropic"
		if modelName == "gpt-4o" {
			providerName = "openai"
		}
		event := &models.ModelUsageEvent{
			ID:           security.NewULID(),
			UserID:       userID,
			ProviderName: providerName,
			ModelName:    modelName,
			PromptTokens: 100,
			TotalTokens:  100,
		}
		err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
			return tx.UpsertModelUsageStatEntry(ctx, event, models.IntervalDaily, startedAt, nil)
		})
		if err != nil {
			t.Fatalf("upsert %s failed: %v", modelName, err)
		}
	}

	// Query all.
	var all []*models.ModelUsageStatEntry
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryModelUsageStatEntries(ctx, store.ModelUsageStatQuery{
			UserID:       userID,
			IntervalType: models.IntervalDaily,
			StartedAt:    startedAt,
			EndedAt:      startedAt.Add(24 * time.Hour),
		}, nil)
		if err != nil {
			return err
		}
		all = result
		return nil
	})
	if err != nil {
		t.Fatalf("query all failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(all))
	}

	// Query filtered by provider.
	providerFilter := "anthropic"
	var filtered []*models.ModelUsageStatEntry
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryModelUsageStatEntries(ctx, store.ModelUsageStatQuery{
			UserID:       userID,
			IntervalType: models.IntervalDaily,
			StartedAt:    startedAt,
			EndedAt:      startedAt.Add(24 * time.Hour),
			ProviderName: &providerFilter,
		}, nil)
		if err != nil {
			return err
		}
		filtered = result
		return nil
	})
	if err != nil {
		t.Fatalf("query filtered failed: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(filtered))
	}
	if filtered[0].ProviderName != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", filtered[0].ProviderName)
	}
}
