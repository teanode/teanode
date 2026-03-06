package fsstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

func TestFSStoreUpsertAndQueryUsage(t *testing.T) {
	s := openFileSystemStore(t)

	userID := "user-1"
	startedAt := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)

	// Upsert first usage.
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.UpsertUsage(ctx, &models.Usage{
			UserID:           userID,
			ProviderName:     "anthropic",
			ModelName:        "claude-sonnet-4-5-20250514",
			IntervalType:     models.IntervalHourly,
			StartedAt:        startedAt,
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			RequestCount:     1,
		}, nil)
	})
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Upsert second (same bucket, should add).
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.UpsertUsage(ctx, &models.Usage{
			UserID:           userID,
			ProviderName:     "anthropic",
			ModelName:        "claude-sonnet-4-5-20250514",
			IntervalType:     models.IntervalHourly,
			StartedAt:        startedAt,
			PromptTokens:     200,
			CompletionTokens: 100,
			TotalTokens:      300,
			RequestCount:     1,
		}, nil)
	})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Query.
	var entries []*models.Usage
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryUsages(ctx, store.UsageQuery{
			UserID:       userID,
			IntervalType: models.IntervalHourly,
			StartedAt:    startedAt,
			EndedAt:      startedAt.Add(time.Hour),
		}, nil)
		entries = result
		return err
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].PromptTokens != 300 {
		t.Errorf("prompt tokens: got %d, want 300", entries[0].PromptTokens)
	}
	if entries[0].RequestCount != 2 {
		t.Errorf("request count: got %d, want 2", entries[0].RequestCount)
	}
}

func TestFSStoreQueryUsageFilters(t *testing.T) {
	s := openFileSystemStore(t)

	userID := "user-1"
	startedAt := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)

	// Insert two different providers/models.
	for _, entry := range []struct {
		provider string
		model    string
	}{
		{"anthropic", "claude-sonnet-4-5-20250514"},
		{"openai", "gpt-4o"},
	} {
		err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
			return tx.UpsertUsage(ctx, &models.Usage{
				UserID:       userID,
				ProviderName: entry.provider,
				ModelName:    entry.model,
				IntervalType: models.IntervalDaily,
				StartedAt:    startedAt,
				PromptTokens: 100,
				TotalTokens:  100,
				RequestCount: 1,
			}, nil)
		})
		if err != nil {
			t.Fatalf("upsert %s failed: %v", entry.model, err)
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
		all = result
		return err
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
		filtered = result
		return err
	})
	if err != nil {
		t.Fatalf("query filtered failed: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered entry, got %d", len(filtered))
	}
}

func TestFSStoreEviction(t *testing.T) {
	s := openFileSystemStore(t)

	userID := "user-1"

	// Insert more than defaultMaxHourlyEntries (168) hourly entries.
	for i := 0; i < 200; i++ {
		startedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
		err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
			return tx.UpsertUsage(ctx, &models.Usage{
				UserID:       userID,
				ProviderName: "anthropic",
				ModelName:    "claude-sonnet-4-5-20250514",
				IntervalType: models.IntervalHourly,
				StartedAt:    startedAt,
				PromptTokens: 10,
				TotalTokens:  10,
				RequestCount: 1,
			}, nil)
		})
		if err != nil {
			t.Fatalf("upsert %d failed: %v", i, err)
		}
	}

	// Query with a wide range — should get at most 168 entries.
	var entries []*models.Usage
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryUsages(ctx, store.UsageQuery{
			UserID:       userID,
			IntervalType: models.IntervalHourly,
			StartedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			EndedAt:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		}, nil)
		entries = result
		return err
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(entries) > 168 {
		t.Errorf("expected at most 168 entries after eviction, got %d", len(entries))
	}
	// Verify oldest entries were evicted (first entry should be hour index 32 = 200-168).
	expectedFirst := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(32 * time.Hour)
	if len(entries) > 0 && !entries[0].StartedAt.Equal(expectedFirst) {
		t.Errorf("expected first entry at %v, got %v", expectedFirst, entries[0].StartedAt)
	}
}

func TestFSStoreEmptyFileReturnsNoError(t *testing.T) {
	s := openFileSystemStore(t)

	var entries []*models.Usage
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryUsages(ctx, store.UsageQuery{
			UserID:       "nonexistent",
			IntervalType: models.IntervalHourly,
			StartedAt:    time.Now().Add(-time.Hour),
			EndedAt:      time.Now(),
		}, nil)
		entries = result
		return err
	})
	if err != nil {
		t.Fatalf("query on empty store should not error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
