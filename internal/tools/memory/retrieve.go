package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/embeddings"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
)

type scoredSnippet struct {
	itemID string
	title  string
	tags   []string
	start  int
	end    int // inclusive
	score  float64
	lines  []string
}

func (self *memoryTool) executeRetrieve(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	if args.Query == "" {
		return "", fmt.Errorf("query is required for retrieve")
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	contextLines := args.ContextLines
	if contextLines < 0 {
		contextLines = 1
	}
	if contextLines == 0 && args.ContextLines == 0 {
		contextLines = 1
	}

	// Fetch items.
	var items []*models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		listOptions := store.MemoryItemListOptions{}
		if len(args.Tags) > 0 {
			listOptions.Tags = &args.Tags
		}
		items, err = tx.ListMemoryItems(ctx, scope, scopeId, listOptions, nil)
		return err
	}); err != nil {
		return "", err
	}

	// Try semantic retrieval if embeddings are available.
	if result, ok := self.trySemanticRetrieve(ctx, items, args.Query, maxResults); ok {
		return result, nil
	}

	// Fall back to keyword retrieval.
	return self.keywordRetrieve(items, args.Query, maxResults, contextLines)
}

// trySemanticRetrieve attempts to use embeddings for ranking. Returns ("", false)
// if embeddings are not configured, the query embedding fails, or too few items
// have stored embeddings.
func (self *memoryTool) trySemanticRetrieve(ctx context.Context, items []*models.MemoryItem, query string, maxResults int) (string, bool) {
	provider, model := providers.EmbeddingProviderFromContext(ctx)
	if provider == nil {
		return "", false
	}

	// Count items with embeddings.
	embeddedCount := 0
	for _, item := range items {
		if item.Embedding != nil && len(*item.Embedding) > 0 {
			embeddedCount++
		}
	}
	// Require at least half of items to have embeddings for semantic to be useful.
	if len(items) > 0 && embeddedCount < (len(items)+1)/2 {
		return "", false
	}

	queryEmbedding, embedError := provider.Embed(ctx, model, query)
	if embedError != nil {
		log.Warningf("semantic retrieve: embedding query failed, falling back to keyword: %v", embedError)
		return "", false
	}

	type semanticResult struct {
		itemID  string
		title   string
		tags    []string
		content string
		score   float64
	}

	var results []semanticResult
	for _, item := range items {
		if item.Embedding == nil || len(*item.Embedding) == 0 {
			continue
		}
		similarity := embeddings.CosineSimilarity(queryEmbedding, *item.Embedding)
		title := ""
		if item.Title != nil {
			title = *item.Title
		}
		content := ""
		if item.Content != nil {
			content = *item.Content
		}
		tags := []string{}
		if item.Tags != nil {
			tags = *item.Tags
		}
		results = append(results, semanticResult{
			itemID:  item.ID,
			title:   title,
			tags:    tags,
			content: content,
			score:   similarity,
		})
	}

	sort.Slice(results, func(indexA, indexB int) bool {
		return results[indexA].score > results[indexB].score
	})

	totalMatches := len(results)
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	type outputSnippet struct {
		ItemID  string   `json:"itemId"`
		Title   string   `json:"title,omitempty"`
		Snippet string   `json:"snippet"`
		Score   float64  `json:"score"`
		Tags    []string `json:"tags,omitempty"`
	}
	outputSnippets := make([]outputSnippet, len(results))
	for index, result := range results {
		snippet := result.content
		if len(snippet) > 1024 {
			snippet = snippet[:1024] + "…"
		}
		outputSnippets[index] = outputSnippet{
			ItemID:  result.itemID,
			Title:   result.title,
			Snippet: snippet,
			Score:   result.score,
			Tags:    result.tags,
		}
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":       "retrieve",
		"method":       "semantic",
		"snippets":     outputSnippets,
		"totalMatches": totalMatches,
	})
	return string(output), true
}

func (self *memoryTool) keywordRetrieve(items []*models.MemoryItem, query string, maxResults int, contextLines int) (string, error) {
	// Tokenise query: split on whitespace, lowercase, discard <3 chars.
	tokens := tokeniseQuery(query)
	if len(tokens) == 0 {
		return "", fmt.Errorf("query contains no significant tokens (words must be 3+ characters)")
	}

	// Score every line in every item.
	type scoredLine struct {
		itemIndex int
		lineIndex int
		score     float64
		fromTitle bool
	}
	var allScored []scoredLine

	type itemMeta struct {
		id    string
		title string
		tags  []string
		lines []string
	}
	metas := make([]itemMeta, len(items))

	for index, item := range items {
		content := ""
		if item.Content != nil {
			content = *item.Content
		}
		lines := strings.Split(content, "\n")
		title := ""
		if item.Title != nil {
			title = *item.Title
		}
		tags := []string{}
		if item.Tags != nil {
			tags = *item.Tags
		}
		metas[index] = itemMeta{id: item.ID, title: title, tags: tags, lines: lines}

		totalLines := len(lines)
		if totalLines == 0 {
			totalLines = 1
		}
		baseScore := 1.0 / float64(totalLines)

		// Score title as a virtual line.
		if title != "" {
			titleLower := strings.ToLower(title)
			titleScore := 0.0
			for _, token := range tokens {
				if strings.Contains(titleLower, token) {
					titleScore += baseScore * 2.0 // title 2x boost
				}
			}
			if titleScore > 0 {
				allScored = append(allScored, scoredLine{itemIndex: index, lineIndex: -1, score: titleScore, fromTitle: true})
			}
		}

		// Score content lines.
		for lineNumber, line := range lines {
			lineLower := strings.ToLower(line)
			lineScore := 0.0
			for _, token := range tokens {
				if strings.Contains(lineLower, token) {
					lineScore += baseScore
				}
			}
			if lineScore > 0 {
				allScored = append(allScored, scoredLine{itemIndex: index, lineIndex: lineNumber, score: lineScore})
			}
		}
	}

	// Sort descending by score.
	sort.Slice(allScored, func(indexA, indexB int) bool {
		return allScored[indexA].score > allScored[indexB].score
	})

	// Build snippets with context merging, grouped by item.
	type lineRange struct {
		start int
		end   int
		score float64
	}
	itemRanges := map[int][]lineRange{}
	for _, scored := range allScored {
		if scored.fromTitle {
			// Title match: include as a range covering line 0 with context.
			start := 0
			end := contextLines
			meta := metas[scored.itemIndex]
			if end >= len(meta.lines) {
				end = len(meta.lines) - 1
			}
			itemRanges[scored.itemIndex] = append(itemRanges[scored.itemIndex], lineRange{start: start, end: end, score: scored.score})
		} else {
			start := scored.lineIndex - contextLines
			if start < 0 {
				start = 0
			}
			end := scored.lineIndex + contextLines
			meta := metas[scored.itemIndex]
			if end >= len(meta.lines) {
				end = len(meta.lines) - 1
			}
			itemRanges[scored.itemIndex] = append(itemRanges[scored.itemIndex], lineRange{start: start, end: end, score: scored.score})
		}
	}

	// Merge overlapping ranges and collect snippets.
	var snippets []scoredSnippet
	for itemIndex, ranges := range itemRanges {
		// Sort by start.
		sort.Slice(ranges, func(indexA, indexB int) bool {
			return ranges[indexA].start < ranges[indexB].start
		})
		// Merge overlapping.
		merged := []lineRange{ranges[0]}
		for _, rng := range ranges[1:] {
			last := &merged[len(merged)-1]
			if rng.start <= last.end+1 {
				if rng.end > last.end {
					last.end = rng.end
				}
				if rng.score > last.score {
					last.score = rng.score
				}
			} else {
				merged = append(merged, rng)
			}
		}
		meta := metas[itemIndex]
		for _, mergedRange := range merged {
			end := mergedRange.end
			if end >= len(meta.lines) {
				end = len(meta.lines) - 1
			}
			snippet := strings.Join(meta.lines[mergedRange.start:end+1], "\n")
			snippets = append(snippets, scoredSnippet{
				itemID: meta.id,
				title:  meta.title,
				tags:   meta.tags,
				start:  mergedRange.start,
				end:    end,
				score:  mergedRange.score,
				lines:  meta.lines[mergedRange.start : end+1],
			})
			_ = snippet
		}
	}

	// Sort descending by score.
	sort.Slice(snippets, func(indexA, indexB int) bool {
		return snippets[indexA].score > snippets[indexB].score
	})

	totalMatches := len(snippets)
	if len(snippets) > maxResults {
		snippets = snippets[:maxResults]
	}

	// Build output.
	type outputSnippet struct {
		ItemID  string   `json:"itemId"`
		Title   string   `json:"title,omitempty"`
		Snippet string   `json:"snippet"`
		Score   float64  `json:"score"`
		Tags    []string `json:"tags,omitempty"`
	}
	outputSnippets := make([]outputSnippet, len(snippets))
	for index, snippet := range snippets {
		outputSnippets[index] = outputSnippet{
			ItemID:  snippet.itemID,
			Title:   snippet.title,
			Snippet: strings.Join(snippet.lines, "\n"),
			Score:   snippet.score,
			Tags:    snippet.tags,
		}
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":       "retrieve",
		"snippets":     outputSnippets,
		"totalMatches": totalMatches,
	})
	return string(output), nil
}

func tokeniseQuery(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var tokens []string
	for _, word := range words {
		if len(word) >= 3 {
			tokens = append(tokens, word)
		}
	}
	return tokens
}
