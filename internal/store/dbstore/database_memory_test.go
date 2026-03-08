package dbstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func TestCreateMemoryItem(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		content := "hello world"
		tags := []string{"test", "example"}
		title := "Test Item"
		item, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-1"),
			Title:   &title,
			Content: &content,
			Tags:    &tags,
		}, nil)
		if err != nil {
			return err
		}
		if item.ID == "" {
			t.Error("expected ULID to be generated")
		}
		if item.CreatedAt == nil || item.ModifiedAt == nil {
			t.Error("expected timestamps to be set")
		}
		if item.Title == nil || *item.Title != "Test Item" {
			t.Errorf("title = %v, want 'Test Item'", item.Title)
		}
		if item.Content == nil || *item.Content != "hello world" {
			t.Error("content mismatch")
		}
		if item.Tags == nil || len(*item.Tags) != 2 {
			t.Error("tags mismatch")
		}
		return nil
	})
	if transactionError != nil {
		t.Fatalf("transaction: %v", transactionError)
	}
}

func TestGetMemoryItem(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	var createdId string
	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		content := "test content"
		item, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-get-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		createdId = item.ID

		// Get existing.
		got, err := transaction.GetMemoryItem(ctx, createdId, nil)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.ID != createdId {
			t.Errorf("id = %q, want %q", got.ID, createdId)
		}

		// Get non-existent.
		_, err = transaction.GetMemoryItem(ctx, "nonexistent-id", nil)
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("get non-existent = %v, want ErrNotFound", err)
		}
		return nil
	})
}

func TestModifyMemoryItem(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		content := "original content"
		item, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-mod-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		modified, err := transaction.ModifyMemoryItem(ctx, item.ID, func(m *models.MemoryItem) error {
			newContent := "updated content"
			m.Content = &newContent
			newTags := []string{"updated"}
			m.Tags = &newTags
			return nil
		}, nil)
		if err != nil {
			t.Fatalf("modify: %v", err)
		}
		if *modified.Content != "updated content" {
			t.Errorf("content = %q, want 'updated content'", *modified.Content)
		}
		if modified.Tags == nil || len(*modified.Tags) != 1 || (*modified.Tags)[0] != "updated" {
			t.Errorf("tags = %v, want [updated]", modified.Tags)
		}
		if !modified.ModifiedAt.After(*item.CreatedAt) && !modified.ModifiedAt.Equal(*item.CreatedAt) {
			t.Error("ModifiedAt should be updated")
		}
		return nil
	})
}

func TestDeleteMemoryItem(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		content := "to be deleted"
		item, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-del-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		if err := transaction.DeleteMemoryItem(ctx, item.ID, nil); err != nil {
			t.Fatalf("delete: %v", err)
		}

		// Get after delete should return not found.
		_, err = transaction.GetMemoryItem(ctx, item.ID, nil)
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("get after delete = %v, want ErrNotFound", err)
		}
		return nil
	})
}

func TestListMemoryItems(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		scope := models.ScopeAgent
		scopeId := "agent-list-1"

		// Create two items in same scope.
		for _, c := range []string{"item one", "item two"} {
			content := c
			_, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
				Scope:   &scope,
				ScopeID: &scopeId,
				Content: &content,
			}, nil)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
		}

		// Create item in different scope.
		otherContent := "other scope"
		_, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeUser),
			ScopeID: ptrto.Value("user-list-1"),
			Content: &otherContent,
		}, nil)
		if err != nil {
			t.Fatalf("create other: %v", err)
		}

		// List should return only items in scope.
		items, err := transaction.ListMemoryItems(ctx, scope, scopeId, store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) < 2 {
			t.Errorf("list = %d items, want >= 2", len(items))
		}

		// Test limit.
		limit := uint64(1)
		items, err = transaction.ListMemoryItems(ctx, scope, scopeId, store.MemoryItemListOptions{Limit: &limit}, nil)
		if err != nil {
			t.Fatalf("list with limit: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("list with limit = %d items, want 1", len(items))
		}

		return nil
	})
}

func TestListMemoryItemsTagFilter(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		scope := models.ScopeAgent
		scopeId := "agent-tags-1"

		content1 := "tagged item"
		tags1 := []string{"important", "work"}
		_, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeId,
			Content: &content1,
			Tags:    &tags1,
		}, nil)
		if err != nil {
			t.Fatalf("create tagged: %v", err)
		}

		content2 := "untagged item"
		_, err = transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeId,
			Content: &content2,
		}, nil)
		if err != nil {
			t.Fatalf("create untagged: %v", err)
		}

		// Filter by tag.
		filterTags := []string{"important"}
		items, err := transaction.ListMemoryItems(ctx, scope, scopeId, store.MemoryItemListOptions{Tags: &filterTags}, nil)
		if err != nil {
			t.Fatalf("list with tag filter: %v", err)
		}
		if len(items) != 1 {
			t.Errorf("tag filter = %d items, want 1", len(items))
		}

		return nil
	})
}

func TestSearchMemoryItems(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		scope := models.ScopeAgent
		scopeId := "agent-search-1"

		content1 := "The user likes cats and kittens"
		title1 := "Pet preferences"
		_, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeId,
			Title:   &title1,
			Content: &content1,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		content2 := "User prefers dark mode"
		title2 := "Editor preferences"
		_, err = transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeId,
			Title:   &title2,
			Content: &content2,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// Search content.
		results, err := transaction.SearchMemoryItems(ctx, scope, scopeId, "cats", store.MemoryItemSearchOptions{}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Error("search for 'cats' should find results")
		}

		// Case-insensitive.
		results, err = transaction.SearchMemoryItems(ctx, scope, scopeId, "DARK MODE", store.MemoryItemSearchOptions{}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Error("case-insensitive search for 'DARK MODE' should find results")
		}

		// Search title.
		results, err = transaction.SearchMemoryItems(ctx, scope, scopeId, "Pet preferences", store.MemoryItemSearchOptions{}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) == 0 {
			t.Error("search for title 'Pet preferences' should find results")
		}

		// Limit.
		limit := uint64(1)
		results, err = transaction.SearchMemoryItems(ctx, scope, scopeId, "prefer", store.MemoryItemSearchOptions{Limit: &limit}, nil)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(results) > 1 {
			t.Errorf("search with limit 1 = %d results, want <= 1", len(results))
		}

		return nil
	})
}

func TestEmbeddingRoundtrip(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	var created *models.MemoryItem
	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		content := "embedded item"
		embedding := []float64{0.1, 0.2, 0.3, -0.5}
		modelName := "openai:text-embedding-3-small"
		now := time.Now()
		item, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
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
	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		item, err := transaction.GetMemoryItem(ctx, created.ID, nil)
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

func TestCrossScopeIsolation(t *testing.T) {
	openedStore := openDatabaseStore(t)
	ctx := context.Background()

	_ = openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		content := "agent-only data"
		_, err := transaction.CreateMemoryItem(ctx, &models.MemoryItem{
			Scope:   ptrto.Value(models.ScopeAgent),
			ScopeID: ptrto.Value("agent-iso-1"),
			Content: &content,
		}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}

		// Different scope ID should not see items.
		items, err := transaction.ListMemoryItems(ctx, models.ScopeAgent, "agent-iso-2", store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("cross-scope list = %d items, want 0", len(items))
		}

		// Different scope type should not see items.
		items, err = transaction.ListMemoryItems(ctx, models.ScopeUser, "agent-iso-1", store.MemoryItemListOptions{}, nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(items) != 0 {
			t.Errorf("cross-scope-type list = %d items, want 0", len(items))
		}

		return nil
	})
}
