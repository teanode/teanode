package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/embeddings"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
)

func (self *memoryTool) executeRetrieve(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	if args.Query == "" {
		return "", fmt.Errorf("query is required for retrieve")
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	// Fetch items.
	var items []*models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		listOptions := store.MemoryItemListOptions{}
		if len(args.Tags) > 0 {
			listOptions.Tags = &args.Tags
		}
		items, err = transaction.ListMemoryItems(ctx, scope, scopeId, listOptions, nil)
		return err
	}); err != nil {
		return "", err
	}

	// Get embedder from runner context.
	var embedder *embeddings.Embedder
	if runner := runners.RunnerFromContext(ctx); runner != nil {
		embedder = runner.Embedder
	}

	results, totalMatches, method, err := embeddings.SearchMemory(ctx, embedder, items, args.Query, maxResults)
	if err != nil {
		return "", err
	}

	// Format output JSON.
	type outputSnippet struct {
		ItemID  string   `json:"itemId"`
		Title   string   `json:"title,omitempty"`
		Snippet string   `json:"snippet"`
		Score   float64  `json:"score"`
		Tags    []string `json:"tags,omitempty"`
	}
	outputSnippets := make([]outputSnippet, len(results))
	for index, result := range results {
		title := ""
		if result.Item.Title != nil {
			title = *result.Item.Title
		}
		tags := []string{}
		if result.Item.Tags != nil {
			tags = *result.Item.Tags
		}
		outputSnippets[index] = outputSnippet{
			ItemID:  result.Item.ID,
			Title:   title,
			Snippet: result.Snippet,
			Score:   result.Score,
			Tags:    tags,
		}
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":       "retrieve",
		"method":       method,
		"snippets":     outputSnippets,
		"totalMatches": totalMatches,
	})
	return string(output), nil
}
