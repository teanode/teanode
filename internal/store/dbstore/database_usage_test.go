package dbstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/valueor"
)

func TestAccumulateUsageAdditivity(t *testing.T) {
	s := openDatabaseStore(t)

	userId := security.NewULID()
	now := time.Now()
	hourStart := timeutil.TruncateToHour(now.In(time.Local))
	bucketStart := time.Date(hourStart.Year(), hourStart.Month(), hourStart.Day(), hourStart.Hour(), 0, 0, 0, time.Local)

	usage1 := &models.Usage{
		UserID:           ptrto.Value(userId),
		ProviderName:     ptrto.Value("anthropic"),
		ModelName:        ptrto.Value("claude-sonnet-4-5-20250514"),
		PromptTokens:     ptrto.Value(uint64(100)),
		CompletionTokens: ptrto.Value(uint64(50)),
		TotalTokens:      ptrto.Value(uint64(150)),
		RequestCount:     ptrto.Value(uint64(1)),
	}

	usage2 := &models.Usage{
		UserID:           ptrto.Value(userId),
		ProviderName:     ptrto.Value("anthropic"),
		ModelName:        ptrto.Value("claude-sonnet-4-5-20250514"),
		PromptTokens:     ptrto.Value(uint64(200)),
		CompletionTokens: ptrto.Value(uint64(100)),
		TotalTokens:      ptrto.Value(uint64(300)),
		RequestCount:     ptrto.Value(uint64(1)),
	}

	// Upsert first usage.
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.AccumulateUsage(ctx, usage1, nil)
	})
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Upsert second usage (should add to existing).
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		return tx.AccumulateUsage(ctx, usage2, nil)
	})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Query and verify additivity.
	var entries []*models.Usage
	err = s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ListUsages(ctx, store.UsageListOptions{
			UserID:       &userId,
			IntervalType: timeutil.IntervalTypeHour,
			StartedAt:    bucketStart,
			EndedAt:      bucketStart.Add(time.Hour),
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
	if valueor.Zero(entry.PromptTokens) != 300 {
		t.Errorf("prompt tokens: got %d, want 300", valueor.Zero(entry.PromptTokens))
	}
	if valueor.Zero(entry.CompletionTokens) != 150 {
		t.Errorf("completion tokens: got %d, want 150", valueor.Zero(entry.CompletionTokens))
	}
	if valueor.Zero(entry.TotalTokens) != 450 {
		t.Errorf("total tokens: got %d, want 450", valueor.Zero(entry.TotalTokens))
	}
	if valueor.Zero(entry.RequestCount) != 2 {
		t.Errorf("request count: got %d, want 2", valueor.Zero(entry.RequestCount))
	}
}

func TestListUsagesFilters(t *testing.T) {
	s := openDatabaseStore(t)

	userId := security.NewULID()
	now := time.Now()
	dayStart := timeutil.TruncateToDay(now.In(time.Local))
	bucketStart := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, time.Local)

	// Insert two different models.
	for _, modelName := range []string{"claude-sonnet-4-5-20250514", "gpt-4o"} {
		providerName := "anthropic"
		if modelName == "gpt-4o" {
			providerName = "openai"
		}
		usage := &models.Usage{
			UserID:       ptrto.Value(userId),
			ProviderName: ptrto.Value(providerName),
			ModelName:    ptrto.Value(modelName),
			PromptTokens: ptrto.Value(uint64(100)),
			TotalTokens:  ptrto.Value(uint64(100)),
			RequestCount: ptrto.Value(uint64(1)),
		}
		err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
			return tx.AccumulateUsage(ctx, usage, nil)
		})
		if err != nil {
			t.Fatalf("upsert %s failed: %v", modelName, err)
		}
	}

	// Query all.
	var all []*models.Usage
	err := s.Transaction(context.Background(), func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ListUsages(ctx, store.UsageListOptions{
			UserID:       &userId,
			IntervalType: timeutil.IntervalTypeDay,
			StartedAt:    bucketStart,
			EndedAt:      bucketStart.Add(24 * time.Hour),
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
		result, err := tx.ListUsages(ctx, store.UsageListOptions{
			UserID:       &userId,
			IntervalType: timeutil.IntervalTypeDay,
			StartedAt:    bucketStart,
			EndedAt:      bucketStart.Add(24 * time.Hour),
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
	if valueor.Zero(filtered[0].ProviderName) != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", valueor.Zero(filtered[0].ProviderName))
	}
}
