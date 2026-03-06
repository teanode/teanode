package dbstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/security"
)

func TestUpsertUsageAdditivity(t *testing.T) {
	s := openDatabaseStore(t)

	userID := security.NewULID()
	startedAt := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	usage1 := &models.Usage{
		UserID:           userID,
		ProviderName:     "anthropic",
		ModelName:        "claude-sonnet-4-5-20250514",
		IntervalType:     models.IntervalHourly,
		StartedAt:        startedAt,
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		RequestCount:     1,
	}

	usage2 := &models.Usage{
		UserID:           userID,
		ProviderName:     "anthropic",
		ModelName:        "claude-sonnet-4-5-20250514",
		IntervalType:     models.IntervalHourly,
		StartedAt:        startedAt,
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
		RequestCount:     1,
	}

	// Upsert first usage.
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.UpsertUsage(ctx, usage1, nil)
	})
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Upsert second usage (should add to existing).
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.UpsertUsage(ctx, usage2, nil)
	})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Query and verify additivity.
	var entries []*models.Usage
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryUsages(ctx, store.UsageQuery{
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

func TestQueryUsagesFilters(t *testing.T) {
	s := openDatabaseStore(t)

	userID := security.NewULID()
	startedAt := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)

	// Insert two different models.
	for _, modelName := range []string{"claude-sonnet-4-5-20250514", "gpt-4o"} {
		providerName := "anthropic"
		if modelName == "gpt-4o" {
			providerName = "openai"
		}
		usage := &models.Usage{
			UserID:       userID,
			ProviderName: providerName,
			ModelName:    modelName,
			IntervalType: models.IntervalDaily,
			StartedAt:    startedAt,
			PromptTokens: 100,
			TotalTokens:  100,
			RequestCount: 1,
		}
		err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
			return tx.UpsertUsage(ctx, usage, nil)
		})
		if err != nil {
			t.Fatalf("upsert %s failed: %v", modelName, err)
		}
	}

	// Query all.
	var all []*models.Usage
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryUsages(ctx, store.UsageQuery{
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
	var filtered []*models.Usage
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryUsages(ctx, store.UsageQuery{
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
