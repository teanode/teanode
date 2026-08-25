// Package toolsearch exposes a tool that loads deferred tool definitions on
// demand. It is registered only when the tool set has been deferred to fit a
// small context window; with the full tool set loaded there is nothing to
// search for.
package toolsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

// ToolName is the name the model calls to load deferred tools.
const ToolName = "tool_search"

// maxActivationsPerCall caps how many tools a single search loads, so that a
// broad query cannot undo the context saving that deferral bought.
const maxActivationsPerCall = 5

// Tool loads deferred tool definitions from a registry on demand.
type Tool struct {
	registry *tools.ToolRegistry
}

// New creates a tool_search tool backed by the given registry.
func New(registry *tools.ToolRegistry) *Tool {
	return &Tool{registry: registry}
}

func (self *Tool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: ToolName,
			Description: "Load tools listed in the 'Additional Tools' catalog so you can call them. " +
				"Their full definitions are left out of this request to save space, so they cannot be called until loaded. " +
				"Pass the exact names when you know them, otherwise pass keywords. " +
				"The loaded tools become callable on your next turn, not in this one.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"names": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Exact tool names from the catalog to load.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Keywords to match against catalog tool names and summaries, used when exact names are unknown.",
					},
				},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"loaded":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Tool names now callable."},
					"matched":   map[string]interface{}{"type": "array", "description": "Matching catalog entries with name and summary."},
					"remaining": map[string]interface{}{"type": "integer", "description": "Catalog entries still not loaded."},
					"message":   map[string]interface{}{"type": "string"},
				},
			},
		},
	}
}

func (self *Tool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

type searchArguments struct {
	Names []string `json:"names"`
	Query string   `json:"query"`
}

type matchedEntry struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type searchResult struct {
	Loaded    []string       `json:"loaded"`
	Matched   []matchedEntry `json:"matched"`
	Remaining int            `json:"remaining"`
	Message   string         `json:"message"`
}

func (self *Tool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments searchArguments
	if strings.TrimSpace(rawArguments) != "" {
		if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
			return "", fmt.Errorf("toolsearch: parsing arguments: %w", err)
		}
	}

	catalog := self.registry.DeferredCatalog()
	matches := selectMatches(catalog, arguments.Names, arguments.Query)
	if len(matches) > maxActivationsPerCall {
		matches = matches[:maxActivationsPerCall]
	}

	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.Name)
	}
	loaded := self.registry.Activate(names)

	result := searchResult{
		Loaded:    loaded,
		Matched:   matches,
		Remaining: len(self.registry.DeferredCatalog()),
	}
	if result.Loaded == nil {
		result.Loaded = []string{}
	}
	if result.Matched == nil {
		result.Matched = []matchedEntry{}
	}
	switch {
	case len(loaded) == 0 && len(catalog) == 0:
		result.Message = "No tools are deferred; everything available is already loaded."
	case len(loaded) == 0:
		result.Message = "No catalog entry matched. Check the 'Additional Tools' list and retry with an exact name."
	default:
		result.Message = fmt.Sprintf("Loaded %d tool(s). They are callable on your next turn.", len(loaded))
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("toolsearch: encoding result: %w", err)
	}
	return string(encoded), nil
}

// selectMatches resolves requested names first, then ranks the remaining
// catalog by how many query terms each entry matches.
func selectMatches(catalog []tools.CatalogEntry, names []string, query string) []matchedEntry {
	byName := make(map[string]tools.CatalogEntry, len(catalog))
	for _, entry := range catalog {
		byName[entry.Name] = entry
	}

	matches := make([]matchedEntry, 0, len(names))
	claimed := make(map[string]bool, len(names))
	for _, name := range names {
		entry, ok := byName[strings.TrimSpace(name)]
		if !ok || claimed[entry.Name] {
			continue
		}
		claimed[entry.Name] = true
		matches = append(matches, matchedEntry{Name: entry.Name, Summary: entry.Summary})
	}

	terms := queryTerms(query)
	if len(terms) == 0 {
		return matches
	}

	type scored struct {
		entry matchedEntry
		score int
	}
	ranked := make([]scored, 0, len(catalog))
	for _, entry := range catalog {
		if claimed[entry.Name] {
			continue
		}
		score := scoreEntry(entry, terms)
		if score > 0 {
			ranked = append(ranked, scored{entry: matchedEntry{Name: entry.Name, Summary: entry.Summary}, score: score})
		}
	}
	// Highest score first, name as the tie-break so results are reproducible.
	sort.SliceStable(ranked, func(left int, right int) bool {
		if ranked[left].score != ranked[right].score {
			return ranked[left].score > ranked[right].score
		}
		return ranked[left].entry.Name < ranked[right].entry.Name
	})
	for _, item := range ranked {
		matches = append(matches, item.entry)
	}
	return matches
}

func queryTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(character rune) bool {
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		return !isLetter && !isDigit
	})
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) > 1 {
			terms = append(terms, field)
		}
	}
	return terms
}

// scoreEntry weights a name match above a summary match so that searching for
// a tool by (part of) its name ranks it first.
func scoreEntry(entry tools.CatalogEntry, terms []string) int {
	name := strings.ToLower(entry.Name)
	summary := strings.ToLower(entry.Summary)
	score := 0
	for _, term := range terms {
		if strings.Contains(name, term) {
			score += 2
		}
		if strings.Contains(summary, term) {
			score++
		}
	}
	return score
}
