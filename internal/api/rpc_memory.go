package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/embeddings"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

// Memory RPC handlers.

type memoryListParameters struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scopeId"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

func (self *webSocketConnection) handleMemoryList(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[memoryListParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.Scope == "" {
		return nil, rpcError(400, "scope is required")
	}
	if parameters.ScopeID == "" {
		return nil, rpcError(400, "scopeId is required")
	}
	scope := models.Scope(parameters.Scope)

	// Access control: non-admin can only access user scope with own userId.
	if !self.isAdmin() && scope == models.ScopeUser && parameters.ScopeID != self.userId() {
		return nil, rpcError(403, "access denied")
	}

	limit := parameters.Limit
	if limit <= 0 {
		limit = 50
	}

	var items []*models.MemoryItem
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		items, err = transaction.ListMemoryItems(ctx, scope, parameters.ScopeID, store.MemoryItemListOptions{}, nil)
		return err
	}); err != nil {
		return nil, rpcError(500, "listing memory items: "+err.Error())
	}

	total := len(items)

	// Apply offset.
	if parameters.Offset > 0 && parameters.Offset < len(items) {
		items = items[parameters.Offset:]
	} else if parameters.Offset >= len(items) {
		items = nil
	}

	// Apply limit.
	if len(items) > limit {
		items = items[:limit]
	}

	itemList := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		entry := map[string]interface{}{
			"id": item.ID,
		}
		if item.Title != nil {
			entry["title"] = *item.Title
		}
		if item.Content != nil {
			entry["content"] = *item.Content
		}
		if item.Tags != nil {
			entry["tags"] = *item.Tags
		}
		if item.Scope != nil {
			entry["scope"] = string(*item.Scope)
		}
		if item.ScopeID != nil {
			entry["scopeId"] = *item.ScopeID
		}
		if item.CreatedAt != nil {
			entry["createdAt"] = item.CreatedAt.Format(time.RFC3339)
		}
		if item.ModifiedAt != nil {
			entry["modifiedAt"] = item.ModifiedAt.Format(time.RFC3339)
		}
		if item.ArchivedAt != nil {
			entry["archivedAt"] = item.ArchivedAt.Format(time.RFC3339)
		}
		itemList = append(itemList, entry)
	}

	return map[string]interface{}{
		"items": itemList,
		"total": total,
	}, nil
}

type memorySearchParameters struct {
	Scope      string `json:"scope"`
	ScopeID    string `json:"scopeId"`
	Query      string `json:"query"`
	MaxResults int    `json:"maxResults"`
}

func (self *webSocketConnection) handleMemorySearch(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[memorySearchParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.Scope == "" || parameters.ScopeID == "" || parameters.Query == "" {
		return nil, rpcError(400, "scope, scopeId, and query are required")
	}
	scope := models.Scope(parameters.Scope)

	// Access control: non-admin can only access user scope with own userId.
	if !self.isAdmin() && scope == models.ScopeUser && parameters.ScopeID != self.userId() {
		return nil, rpcError(403, "access denied")
	}

	maxResults := parameters.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}

	var items []*models.MemoryItem
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		items, err = transaction.ListMemoryItems(ctx, scope, parameters.ScopeID, store.MemoryItemListOptions{}, nil)
		return err
	}); err != nil {
		return nil, rpcError(500, "listing memory items: "+err.Error())
	}

	// Create embedder from provider registry.
	var embedder *embeddings.Embedder
	if providerRegistry := self.api.coordinator.ProviderRegistry(); providerRegistry != nil {
		embedder = embeddings.NewEmbedder(providerRegistry)
	}

	results, totalMatches, method, err := embeddings.SearchMemory(self.ctx, embedder, items, parameters.Query, maxResults)
	if err != nil {
		return nil, rpcError(500, "searching memory: "+err.Error())
	}

	snippets := make([]map[string]interface{}, 0, len(results))
	for _, result := range results {
		item := result.Item
		title := ""
		if item.Title != nil {
			title = *item.Title
		}
		tags := []string{}
		if item.Tags != nil {
			tags = *item.Tags
		}
		content := ""
		if item.Content != nil {
			content = *item.Content
		}
		entry := map[string]interface{}{
			"itemId":  item.ID,
			"title":   title,
			"snippet": result.Snippet,
			"content": content,
			"score":   result.Score,
			"tags":    tags,
		}
		if item.ModifiedAt != nil {
			entry["modifiedAt"] = item.ModifiedAt.Format(time.RFC3339)
		}
		snippets = append(snippets, entry)
	}

	return map[string]interface{}{
		"snippets":     snippets,
		"totalMatches": totalMatches,
		"method":       string(method),
	}, nil
}

type memoryDeleteParameters struct {
	MemoryItemID string `json:"memoryItemId"`
}

func (self *webSocketConnection) handleMemoryDelete(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[memoryDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.MemoryItemID == "" {
		return nil, rpcError(400, "memoryItemId is required")
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		// Fetch item to check ownership.
		item, err := transaction.GetMemoryItem(ctx, parameters.MemoryItemID, nil)
		if err != nil {
			return err
		}

		// Access control: non-admin can only delete user scope with own userId.
		if !self.isAdmin() {
			if item.Scope == nil || *item.Scope != models.ScopeUser {
				return fmt.Errorf("api: access denied: can only delete user-scope items")
			}
			if item.ScopeID == nil || *item.ScopeID != self.userId() {
				return fmt.Errorf("api: access denied: item belongs to another user")
			}
		}

		return transaction.DeleteMemoryItem(ctx, parameters.MemoryItemID, nil)
	}); err != nil {
		if strings.Contains(err.Error(), "access denied") {
			return nil, rpcError(403, err.Error())
		}
		return nil, rpcError(500, "deleting memory item: "+err.Error())
	}

	return map[string]interface{}{
		"deleted": true,
	}, nil
}
