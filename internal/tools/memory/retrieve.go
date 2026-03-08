package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
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

	// Tokenise query: split on whitespace, lowercase, discard <3 chars.
	tokens := tokeniseQuery(args.Query)
	if len(tokens) == 0 {
		return "", fmt.Errorf("query contains no significant tokens (words must be 3+ characters)")
	}

	// Fetch items.
	var items []*models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		listOpts := store.MemoryItemListOptions{}
		if len(args.Tags) > 0 {
			listOpts.Tags = &args.Tags
		}
		items, err = tx.ListMemoryItems(ctx, scope, scopeId, listOpts, nil)
		return err
	}); err != nil {
		return "", err
	}

	// Score every line in every item.
	type scoredLine struct {
		itemIdx   int
		lineIdx   int
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

	for i, item := range items {
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
		metas[i] = itemMeta{id: item.ID, title: title, tags: tags, lines: lines}

		totalLines := len(lines)
		if totalLines == 0 {
			totalLines = 1
		}
		baseScore := 1.0 / float64(totalLines)

		// Score title as a virtual line.
		if title != "" {
			titleLower := strings.ToLower(title)
			s := 0.0
			for _, tok := range tokens {
				if strings.Contains(titleLower, tok) {
					s += baseScore * 2.0 // title 2x boost
				}
			}
			if s > 0 {
				allScored = append(allScored, scoredLine{itemIdx: i, lineIdx: -1, score: s, fromTitle: true})
			}
		}

		// Score content lines.
		for li, line := range lines {
			lineLower := strings.ToLower(line)
			s := 0.0
			for _, tok := range tokens {
				if strings.Contains(lineLower, tok) {
					s += baseScore
				}
			}
			if s > 0 {
				allScored = append(allScored, scoredLine{itemIdx: i, lineIdx: li, score: s})
			}
		}
	}

	// Sort descending by score.
	sort.Slice(allScored, func(a, b int) bool {
		return allScored[a].score > allScored[b].score
	})

	// Build snippets with context merging, grouped by item.
	type lineRange struct {
		start int
		end   int
		score float64
	}
	itemRanges := map[int][]lineRange{}
	for _, sl := range allScored {
		if sl.fromTitle {
			// Title match: include as a range covering line 0 with context.
			start := 0
			end := contextLines
			m := metas[sl.itemIdx]
			if end >= len(m.lines) {
				end = len(m.lines) - 1
			}
			itemRanges[sl.itemIdx] = append(itemRanges[sl.itemIdx], lineRange{start: start, end: end, score: sl.score})
		} else {
			start := sl.lineIdx - contextLines
			if start < 0 {
				start = 0
			}
			end := sl.lineIdx + contextLines
			m := metas[sl.itemIdx]
			if end >= len(m.lines) {
				end = len(m.lines) - 1
			}
			itemRanges[sl.itemIdx] = append(itemRanges[sl.itemIdx], lineRange{start: start, end: end, score: sl.score})
		}
	}

	// Merge overlapping ranges and collect snippets.
	var snippets []scoredSnippet
	for itemIdx, ranges := range itemRanges {
		// Sort by start.
		sort.Slice(ranges, func(a, b int) bool {
			return ranges[a].start < ranges[b].start
		})
		// Merge overlapping.
		merged := []lineRange{ranges[0]}
		for _, r := range ranges[1:] {
			last := &merged[len(merged)-1]
			if r.start <= last.end+1 {
				if r.end > last.end {
					last.end = r.end
				}
				if r.score > last.score {
					last.score = r.score
				}
			} else {
				merged = append(merged, r)
			}
		}
		m := metas[itemIdx]
		for _, mr := range merged {
			end := mr.end
			if end >= len(m.lines) {
				end = len(m.lines) - 1
			}
			snippet := strings.Join(m.lines[mr.start:end+1], "\n")
			snippets = append(snippets, scoredSnippet{
				itemID: m.id,
				title:  m.title,
				tags:   m.tags,
				start:  mr.start,
				end:    end,
				score:  mr.score,
				lines:  m.lines[mr.start : end+1],
			})
			_ = snippet
		}
	}

	// Sort descending by score.
	sort.Slice(snippets, func(a, b int) bool {
		return snippets[a].score > snippets[b].score
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
	outSnippets := make([]outputSnippet, len(snippets))
	for i, s := range snippets {
		outSnippets[i] = outputSnippet{
			ItemID:  s.itemID,
			Title:   s.title,
			Snippet: strings.Join(s.lines, "\n"),
			Score:   s.score,
			Tags:    s.tags,
		}
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":       "retrieve",
		"snippets":     outSnippets,
		"totalMatches": totalMatches,
	})
	return string(output), nil
}

func tokeniseQuery(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var tokens []string
	for _, w := range words {
		if len(w) >= 3 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}
