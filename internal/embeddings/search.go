package embeddings

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/models"
)

var log = logging.MustGetLogger("embeddings")

// SearchMethod indicates how a search result was found.
type SearchMethod string

const (
	SearchMethodSemantic SearchMethod = "semantic"
	SearchMethodKeyword  SearchMethod = "keyword"
)

// MemorySearchResult represents a ranked search result.
type MemorySearchResult struct {
	Item    *models.MemoryItem
	Snippet string
	Score   float64
	Method  SearchMethod
}

// SearchMemory tries semantic search first, falls back to keyword search.
// Returns results, total match count, method name, and error.
func SearchMemory(ctx context.Context, embedder *Embedder, items []*models.MemoryItem, query string, maxResults int) ([]MemorySearchResult, int, SearchMethod, error) {
	results, totalMatches, ok := SemanticSearchMemory(ctx, embedder, items, query, maxResults)
	if ok {
		return results, totalMatches, SearchMethodSemantic, nil
	}

	results, totalMatches, err := KeywordSearchMemory(items, query, maxResults, 1)
	if err != nil {
		return nil, 0, "", err
	}
	return results, totalMatches, SearchMethodKeyword, nil
}

// SemanticSearchMemory ranks items by cosine similarity to the query embedding.
// Returns (nil, 0, false) if embeddings are unavailable or insufficient.
func SemanticSearchMemory(ctx context.Context, embedder *Embedder, items []*models.MemoryItem, query string, maxResults int) ([]MemorySearchResult, int, bool) {
	if embedder == nil {
		return nil, 0, false
	}

	queryEmbedding, queryProviderModelName, err := embedder.Embed(ctx, query)
	if err != nil {
		log.Warningf("semantic search: embedding query failed, falling back to keyword: %v", err)
		return nil, 0, false
	}

	// Count items with embeddings from the same provider model.
	matchingCount := 0
	for _, item := range items {
		if item.Embedding != nil && len(*item.Embedding) > 0 &&
			item.EmbeddingProviderModelName != nil && *item.EmbeddingProviderModelName == queryProviderModelName {
			matchingCount++
		}
	}
	// Require at least half of items to have matching embeddings for semantic to be useful.
	if len(items) > 0 && matchingCount < (len(items)+1)/2 {
		return nil, 0, false
	}

	var results []MemorySearchResult
	for _, item := range items {
		if item.Embedding == nil || len(*item.Embedding) == 0 {
			continue
		}
		if item.EmbeddingProviderModelName == nil || *item.EmbeddingProviderModelName != queryProviderModelName {
			continue
		}
		similarity := CosineSimilarity(queryEmbedding, *item.Embedding)
		content := ""
		if item.Content != nil {
			content = *item.Content
		}
		// Truncate content to 1024 characters.
		if len(content) > 1024 {
			content = content[:1024] + "\u2026"
		}
		results = append(results, MemorySearchResult{
			Item:    item,
			Snippet: content,
			Score:   similarity,
			Method:  SearchMethodSemantic,
		})
	}

	sort.Slice(results, func(indexA, indexB int) bool {
		return results[indexA].Score > results[indexB].Score
	})

	totalMatches := len(results)
	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results, totalMatches, true
}

// KeywordSearchMemory scores items by keyword token matching with context merging.
func KeywordSearchMemory(items []*models.MemoryItem, query string, maxResults int, contextLines int) ([]MemorySearchResult, int, error) {
	tokens := tokeniseQuery(query)
	if len(tokens) == 0 {
		return nil, 0, fmt.Errorf("query contains no significant tokens (words must be 3+ characters)")
	}

	type scoredLine struct {
		itemIndex int
		lineIndex int
		score     float64
		fromTitle bool
	}
	var allScored []scoredLine

	type itemMeta struct {
		lines []string
		title string
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
		metas[index] = itemMeta{lines: lines, title: title}

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
	type snippet struct {
		itemIndex int
		score     float64
		lines     []string
	}
	var snippets []snippet
	for itemIndex, ranges := range itemRanges {
		sort.Slice(ranges, func(indexA, indexB int) bool {
			return ranges[indexA].start < ranges[indexB].start
		})
		merged := []lineRange{ranges[0]}
		for _, current := range ranges[1:] {
			last := &merged[len(merged)-1]
			if current.start <= last.end+1 {
				if current.end > last.end {
					last.end = current.end
				}
				if current.score > last.score {
					last.score = current.score
				}
			} else {
				merged = append(merged, current)
			}
		}
		meta := metas[itemIndex]
		for _, mergedRange := range merged {
			end := mergedRange.end
			if end >= len(meta.lines) {
				end = len(meta.lines) - 1
			}
			snippets = append(snippets, snippet{
				itemIndex: itemIndex,
				score:     mergedRange.score,
				lines:     meta.lines[mergedRange.start : end+1],
			})
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

	results := make([]MemorySearchResult, len(snippets))
	for index, matched := range snippets {
		results[index] = MemorySearchResult{
			Item:    items[matched.itemIndex],
			Snippet: strings.Join(matched.lines, "\n"),
			Score:   matched.score,
			Method:  SearchMethodKeyword,
		}
	}

	return results, totalMatches, nil
}

// tokeniseQuery splits query into lowercase tokens of 3+ characters.
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
