package fsstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func setupMemoryTestStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, err := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	if err := openedStore.Migrate(context.Background()); err != nil {
		t.Fatalf("migrating store: %v", err)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

func TestMemoryCreateAndGet(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	var created *models.MemoryItem
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "hello world"
		tags := []string{"test", "example"}
		title := "Test Item"
		item, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-1"),
			Title:   &title,
			Content: &content,
			Tags:    &tags,
		}, nil)
		if err != nil {
			return err
		}
		created = item
		return nil
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.ID == "" {
		t.Error("expected ULID to be generated")
	}
	if created.CreatedAt == nil || created.ModifiedAt == nil {
		t.Error("expected timestamps to be set")
	}
	if created.Title == nil || *created.Title != "Test Item" {
		t.Errorf("title = %v, want 'Test Item'", created.Title)
	}
	if created.Content == nil || *created.Content != "hello world" {
		t.Error("content mismatch")
	}
	if created.Tags == nil || len(*created.Tags) != 2 {
		t.Error("tags mismatch")
	}

	// Get it back.
	var fetched *models.MemoryItem
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		item, err := tx.GetMemoryItem(ctx, created.ID, nil)
		if err != nil {
			return err
		}
		fetched = item
		return nil
	}); err != nil {
		t.Fatalf("get: %v", err)
	}

	if fetched.ID != created.ID {
		t.Errorf("id = %q, want %q", fetched.ID, created.ID)
	}
	if fetched.Content == nil || *fetched.Content != "hello world" {
		t.Error("content mismatch after get")
	}
}

func TestMemoryGetNotFound(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.GetMemoryItem(ctx, "nonexistent-id", nil)
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get non-existent = %v, want ErrNotFound", err)
	}
}

func TestMemoryModify(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	var created *models.MemoryItem
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "original content"
		item, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-mod-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		created = item
		return nil
	})

	var modified *models.MemoryItem
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		item, err := tx.ModifyMemoryItem(ctx, created.ID, func(m *models.MemoryItem) error {
			newContent := "updated content"
			m.Content = &newContent
			newTags := []string{"updated"}
			m.Tags = &newTags
			return nil
		}, nil)
		if err != nil {
			return err
		}
		modified = item
		return nil
	}); err != nil {
		t.Fatalf("modify: %v", err)
	}

	if *modified.Content != "updated content" {
		t.Errorf("content = %q, want 'updated content'", *modified.Content)
	}
	if modified.Tags == nil || len(*modified.Tags) != 1 || (*modified.Tags)[0] != "updated" {
		t.Errorf("tags = %v, want [updated]", modified.Tags)
	}
}

func TestMemoryDelete(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	var created *models.MemoryItem
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "to be deleted"
		item, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-del-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		created = item
		return nil
	})

	// Delete it.
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteMemoryItem(ctx, created.ID, nil)
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Get after delete should return not found.
	err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.GetMemoryItem(ctx, created.ID, nil)
		return err
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestMemoryListItems(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		scope := models.ScopeAgent
		scopeID := "agent-list-1"
		for _, c := range []string{"item one", "item two"} {
			content := c
			_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
				Scope:   &scope,
				ScopeID: &scopeID,
				Content: &content,
			}, nil)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
		}

		// Create item in different scope.
		otherContent := "other scope"
		_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeUser),
			ScopeID: ptrto.Value("user-list-1"),
			Content: &otherContent,
		}, nil)
		if err != nil {
			t.Fatalf("create other: %v", err)
		}

		// List should return only items in scope.
		items, err := tx.ListMemoryItems(ctx, scope, scopeID, store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 2 {
			t.Errorf("list = %d items, want 2", len(items))
		}

		// Test limit.
		limit := uint64(1)
		items, err = tx.ListMemoryItems(ctx, scope, scopeID, store.MemoryItemListOptions{Limit: &limit}, nil)
		if err != nil {
			t.Fatalf("list with limit: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("list with limit = %d items, want 1", len(items))
		}
		return nil
	})
}

func TestMemoryListTagFilter(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		scope := models.ScopeAgent
		scopeID := "agent-tags-1"

		content1 := "tagged item"
		tags1 := []string{"important", "work"}
		_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeID,
			Content: &content1,
			Tags:    &tags1,
		}, nil)
		if err != nil {
			t.Fatalf("create tagged: %v", err)
		}

		content2 := "untagged item"
		_, err = tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeID,
			Content: &content2,
		}, nil)
		if err != nil {
			t.Fatalf("create untagged: %v", err)
		}

		filterTags := []string{"important"}
		items, err := tx.ListMemoryItems(ctx, scope, scopeID, store.MemoryItemListOptions{Tags: &filterTags}, nil)
		if err != nil {
			t.Fatalf("list with tag filter: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("tag filter = %d items, want 1", len(items))
		}
		return nil
	})
}

func TestMemorySearch(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		scope := models.ScopeAgent
		scopeID := "agent-search-1"

		content1 := "The user likes cats and kittens"
		title1 := "Pet preferences"
		_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeID,
			Title:   &title1,
			Content: &content1,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		content2 := "User prefers dark mode"
		title2 := "Editor preferences"
		_, err = tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeID,
			Title:   &title2,
			Content: &content2,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// Search content.
		results, err := tx.SearchMemoryItems(ctx, scope, scopeID, "cats", store.MemoryItemSearchOptions{}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Error("search for 'cats' should find results")
		}

		// Case-insensitive.
		results, err = tx.SearchMemoryItems(ctx, scope, scopeID, "DARK MODE", store.MemoryItemSearchOptions{}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Error("case-insensitive search for 'DARK MODE' should find results")
		}

		// Search title.
		results, err = tx.SearchMemoryItems(ctx, scope, scopeID, "Pet preferences", store.MemoryItemSearchOptions{}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Error("search for title 'Pet preferences' should find results")
		}

		// Limit.
		limit := uint64(1)
		results, err = tx.SearchMemoryItems(ctx, scope, scopeID, "prefer", store.MemoryItemSearchOptions{Limit: &limit}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) > 1 {
			t.Errorf("search with limit 1 = %d results, want <= 1", len(results))
		}

		return nil
	})
}

func TestMemoryCrossScopeIsolation(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "agent-only data"
		_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-iso-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// Different scope ID should not see items.
		items, err := tx.ListMemoryItems(ctx, models.ScopeAgent, "agent-iso-2", store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("cross-scope list = %d items, want 0", len(items))
		}

		// Different scope type should not see items.
		items, err = tx.ListMemoryItems(ctx, models.ScopeUser, "agent-iso-1", store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("cross-scope-type list = %d items, want 0", len(items))
		}
		return nil
	})
}

func TestMemoryUserScope(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "user memory"
		_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeUser),
			ScopeID: ptrto.Value("user-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		items, err := tx.ListMemoryItems(ctx, models.ScopeUser, "user-1", store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("list = %d items, want 1", len(items))
		}
		return nil
	})
}

func TestMemoryProjectScope(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "project memory"
		_, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeProject),
			ScopeID: ptrto.Value("project-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		items, err := tx.ListMemoryItems(ctx, models.ScopeProject, "project-1", store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("list = %d items, want 1", len(items))
		}
		return nil
	})
}

func TestMemoryEmbeddingRoundtrip(t *testing.T) {
	s := setupMemoryTestStore(t)
	ctx := context.Background()

	var created *models.MemoryItem
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		content := "embedded item"
		embedding := []float64{0.1, 0.2, 0.3, -0.5}
		modelName := "openai:text-embedding-3-small"
		now := time.Now()
		item, err := tx.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:                      ptrto.Value(models.ScopeAgent),
			ScopeID:                    ptrto.Value("agent-embed-1"),
			Content:                    &content,
			EmbeddingProviderModelName: &modelName,
			Embedding:                  &embedding,
			EmbeddedAt:                 &now,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		created = item
		return nil
	})

	// Read back and verify embedding roundtrips.
	var fetched *models.MemoryItem
	_ = s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		item, err := tx.GetMemoryItem(ctx, created.ID, nil)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		fetched = item
		return nil
	})

	if fetched.EmbeddingProviderModelName == nil || *fetched.EmbeddingProviderModelName != "openai:text-embedding-3-small" {
		t.Errorf("embeddingProviderModelName = %v, want openai:text-embedding-3-small", fetched.EmbeddingProviderModelName)
	}
	if fetched.Embedding == nil {
		t.Fatal("expected embedding to be persisted")
	}
	expected := []float64{0.1, 0.2, 0.3, -0.5}
	if len(*fetched.Embedding) != len(expected) {
		t.Fatalf("embedding length = %d, want %d", len(*fetched.Embedding), len(expected))
	}
	for index, value := range expected {
		if (*fetched.Embedding)[index] != value {
			t.Errorf("embedding[%d] = %f, want %f", index, (*fetched.Embedding)[index], value)
		}
	}
	if fetched.EmbeddedAt == nil {
		t.Error("expected embeddedAt to be set")
	}
}
