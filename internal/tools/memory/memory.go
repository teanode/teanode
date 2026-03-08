package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("memory")

const maxContentSize = 64 * 1024 // 64 KB
const maxBatchItems = 50

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	return []tools.Tool{
		newMemoryTool(memoryToolConfiguration{
			name:        "agent_memory",
			description: "Persistent per-agent memory for storing and retrieving durable knowledge items.",
			resolveScope: func(ctx context.Context, _ string) (models.Scope, string, error) {
				runner := runners.RunnerFromContext(ctx)
				if runner == nil || runner.AgentID == "" {
					return "", "", fmt.Errorf("missing runner context")
				}
				return models.ScopeAgent, runner.AgentID, nil
			},
			afterMutate: func(ctx context.Context, scopeID string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyAgent(ctx, scopeID, func(agent *models.Agent) error {
						now := time.Now()
						agent.ModifiedAt = &now
						return nil
					}, nil)
					return modifyError
				})
			},
		}),
		newMemoryTool(memoryToolConfiguration{
			name:        "user_memory",
			description: "Persistent per-user memory for storing and retrieving user-specific knowledge.",
			resolveScope: func(ctx context.Context, _ string) (models.Scope, string, error) {
				user := models.UserFromContext(ctx)
				if user == nil || user.ID == "" {
					return "", "", fmt.Errorf("missing user context")
				}
				return models.ScopeUser, user.ID, nil
			},
			afterMutate: func(ctx context.Context, scopeID string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyUser(ctx, scopeID, func(user *models.User) error {
						now := time.Now()
						user.ModifiedAt = &now
						return nil
					}, nil)
					return modifyError
				})
			},
		}),
		newMemoryTool(memoryToolConfiguration{
			name:                        "project_memory",
			description:                 "Persistent per-project memory for storing and retrieving shared project knowledge.",
			scopeIDParameterName:        "projectId",
			scopeIDParameterDescription: "Project ID for project memory operations.",
			resolveScope: func(_ context.Context, scopeID string) (models.Scope, string, error) {
				if scopeID == "" {
					return "", "", fmt.Errorf("projectId is required")
				}
				return models.ScopeProject, scopeID, nil
			},
			afterMutate: func(ctx context.Context, scopeID string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyProject(ctx, scopeID, func(project *models.Project) error {
						now := time.Now()
						project.ModifiedAt = &now
						return nil
					}, nil)
					return modifyError
				})
			},
		}),
	}
}

type memoryToolConfiguration struct {
	name                        string
	description                 string
	scopeIDParameterName        string
	scopeIDParameterDescription string
	resolveScope                func(ctx context.Context, scopeID string) (models.Scope, string, error)
	afterMutate                 func(ctx context.Context, scopeID string) error
}

type memoryTool struct {
	configuration memoryToolConfiguration
}

func newMemoryTool(configuration memoryToolConfiguration) *memoryTool {
	return &memoryTool{configuration: configuration}
}

type batchItem struct {
	Op         string   `json:"op"`
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Query      string   `json:"query"`
	MaxResults int      `json:"maxResults"`
}

type batchResult struct {
	Index   int                      `json:"index"`
	Op      string                   `json:"op"`
	Success bool                     `json:"success"`
	Item    map[string]interface{}   `json:"item,omitempty"`
	Items   []map[string]interface{} `json:"items,omitempty"`
	Matches interface{}              `json:"matches,omitempty"`
	Error   string                   `json:"error,omitempty"`
}

type batchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

func (self *memoryTool) Definition() providers.ToolDefinition {
	ops := []string{"add", "update", "delete", "get", "list", "search"}
	properties := map[string]interface{}{
		"action": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"batch"},
			"description": "The memory action to perform. Only 'batch' is supported.",
		},
		"items": map[string]interface{}{
			"type":        "array",
			"minItems":    1,
			"maxItems":    maxBatchItems,
			"description": "Array of operations to execute (1-50).",
			"items": map[string]interface{}{
				"type":     "object",
				"required": []string{"op"},
				"properties": map[string]interface{}{
					"op": map[string]interface{}{
						"type":        "string",
						"enum":        ops,
						"description": "Operation type.",
					},
					"id": map[string]interface{}{
						"type":        "string",
						"description": "Memory item ID (get/update/delete).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Optional title for a memory item.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to store or update.",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Tags for organization and filtering.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query string (search op).",
					},
					"maxResults": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum results to return, default 10 (list/search).",
					},
				},
			},
		},
	}

	required := []string{"action", "items"}

	if self.configuration.scopeIDParameterName != "" {
		properties[self.configuration.scopeIDParameterName] = map[string]interface{}{
			"type":        "string",
			"description": self.configuration.scopeIDParameterDescription,
		}
		required = append(required, self.configuration.scopeIDParameterName)
	}

	descriptionSuffix := " Batch-only: pass action 'batch' with an items array of operations (add, update, delete, get, list, search)."

	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        self.configuration.name,
			Description: self.configuration.description + descriptionSuffix,
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": properties,
				"required":   required,
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Batch result: {action, results: [{index, op, success, item?, items?, matches?, error?}], summary: {total, succeeded, failed}}.",
			},
		},
	}
}

func (self *memoryTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action  string      `json:"action"`
		Items   []batchItem `json:"items"`
		ScopeID string      `json:"-"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	if self.configuration.scopeIDParameterName != "" {
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rawArguments), &rawMap); err == nil {
			if raw, ok := rawMap[self.configuration.scopeIDParameterName]; ok {
				var scopeID string
				if err := json.Unmarshal(raw, &scopeID); err == nil {
					arguments.ScopeID = scopeID
				}
			}
		}
	}

	scope, scopeID, scopeError := self.configuration.resolveScope(ctx, arguments.ScopeID)
	if scopeError != nil {
		return "", scopeError
	}

	if arguments.Action != "batch" {
		return "", fmt.Errorf("unknown action %q: only 'batch' is supported", arguments.Action)
	}

	return self.executeBatch(ctx, scope, scopeID, arguments.Items)
}

func (self *memoryTool) executeBatch(ctx context.Context, scope models.Scope, scopeID string, items []batchItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("items is required and must contain 1-%d entries", maxBatchItems)
	}
	if len(items) > maxBatchItems {
		return "", fmt.Errorf("items must contain at most %d entries, got %d", maxBatchItems, len(items))
	}

	results := make([]batchResult, len(items))
	succeeded := 0
	anyMutation := false

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		for i, item := range items {
			results[i] = self.executeBatchItem(ctx, tx, scope, scopeID, i, item)
			if results[i].Success {
				succeeded++
			}
			if results[i].Success && isMutatingOp(item.Op) {
				anyMutation = true
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if anyMutation {
		self.callAfterMutate(ctx, scopeID)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":  "batch",
		"results": results,
		"summary": batchSummary{
			Total:     len(items),
			Succeeded: succeeded,
			Failed:    len(items) - succeeded,
		},
	})
	return string(output), nil
}

func isMutatingOp(op string) bool {
	return op == "add" || op == "update" || op == "delete"
}

func (self *memoryTool) executeBatchItem(ctx context.Context, tx store.Transaction, scope models.Scope, scopeID string, index int, item batchItem) batchResult {
	switch item.Op {
	case "add":
		return self.batchAdd(ctx, tx, scope, scopeID, index, item)
	case "update":
		return self.batchUpdate(ctx, tx, index, item)
	case "delete":
		return self.batchDelete(ctx, tx, index, item)
	case "get":
		return self.batchGet(ctx, tx, index, item)
	case "list":
		return self.batchList(ctx, tx, scope, scopeID, index, item)
	case "search":
		return self.batchSearch(ctx, tx, scope, scopeID, index, item)
	default:
		return batchResult{Index: index, Op: item.Op, Success: false, Error: fmt.Sprintf("unknown op: %s", item.Op)}
	}
}

func (self *memoryTool) batchAdd(ctx context.Context, tx store.Transaction, scope models.Scope, scopeID string, index int, item batchItem) batchResult {
	if item.Content == "" {
		return batchResult{Index: index, Op: "add", Success: false, Error: "content is required for add"}
	}
	if len(item.Content) > maxContentSize {
		return batchResult{Index: index, Op: "add", Success: false, Error: fmt.Sprintf("content exceeds maximum size of %d bytes", maxContentSize)}
	}

	memItem := &models.MemoryItem{
		Scope:   &scope,
		ScopeID: &scopeID,
		Content: &item.Content,
	}
	if item.Title != "" {
		memItem.Title = &item.Title
	}
	if len(item.Tags) > 0 {
		memItem.Tags = &item.Tags
	}

	created, err := tx.CreateMemoryItem(ctx, memItem, nil)
	if err != nil {
		return batchResult{Index: index, Op: "add", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "add", Success: true, Item: memoryItemToOutput(created)}
}

func (self *memoryTool) batchUpdate(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.ID == "" {
		return batchResult{Index: index, Op: "update", Success: false, Error: "id is required for update"}
	}
	if item.Content != "" && len(item.Content) > maxContentSize {
		return batchResult{Index: index, Op: "update", Success: false, Error: fmt.Sprintf("content exceeds maximum size of %d bytes", maxContentSize)}
	}

	updated, err := tx.ModifyMemoryItem(ctx, item.ID, func(mem *models.MemoryItem) error {
		if item.Title != "" {
			mem.Title = &item.Title
		}
		if item.Content != "" {
			mem.Content = &item.Content
		}
		if item.Tags != nil {
			mem.Tags = &item.Tags
		}
		return nil
	}, nil)
	if err != nil {
		return batchResult{Index: index, Op: "update", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "update", Success: true, Item: memoryItemToOutput(updated)}
}

func (self *memoryTool) batchDelete(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.ID == "" {
		return batchResult{Index: index, Op: "delete", Success: false, Error: "id is required for delete"}
	}
	if err := tx.DeleteMemoryItem(ctx, item.ID, nil); err != nil {
		return batchResult{Index: index, Op: "delete", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "delete", Success: true}
}

func (self *memoryTool) batchGet(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.ID == "" {
		return batchResult{Index: index, Op: "get", Success: false, Error: "id is required for get"}
	}
	mem, err := tx.GetMemoryItem(ctx, item.ID, nil)
	if err != nil {
		return batchResult{Index: index, Op: "get", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "get", Success: true, Item: memoryItemToOutput(mem)}
}

func (self *memoryTool) batchList(ctx context.Context, tx store.Transaction, scope models.Scope, scopeID string, index int, item batchItem) batchResult {
	maxResults := item.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	limit := uint64(maxResults)

	listOptions := store.MemoryItemListOptions{
		Limit: &limit,
	}
	if len(item.Tags) > 0 {
		listOptions.Tags = &item.Tags
	}

	items, err := tx.ListMemoryItems(ctx, scope, scopeID, listOptions, nil)
	if err != nil {
		return batchResult{Index: index, Op: "list", Success: false, Error: err.Error()}
	}

	outputItems := make([]map[string]interface{}, 0, len(items))
	for _, mem := range items {
		entry := map[string]interface{}{
			"id": mem.ID,
		}
		if mem.Title != nil {
			entry["title"] = *mem.Title
		}
		if mem.Tags != nil {
			entry["tags"] = *mem.Tags
		}
		if mem.CreatedAt != nil {
			entry["createdAt"] = mem.CreatedAt.Format(time.RFC3339)
		}
		if mem.ModifiedAt != nil {
			entry["modifiedAt"] = mem.ModifiedAt.Format(time.RFC3339)
		}
		outputItems = append(outputItems, entry)
	}
	return batchResult{Index: index, Op: "list", Success: true, Items: outputItems}
}

func (self *memoryTool) batchSearch(ctx context.Context, tx store.Transaction, scope models.Scope, scopeID string, index int, item batchItem) batchResult {
	if item.Query == "" {
		return batchResult{Index: index, Op: "search", Success: false, Error: "query is required for search"}
	}
	maxResults := item.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	limit := uint64(maxResults)
	includeContent := true

	type matchEntry struct {
		ID      string   `json:"id"`
		Title   string   `json:"title,omitempty"`
		Snippet string   `json:"snippet,omitempty"`
		Tags    []string `json:"tags,omitempty"`
	}

	results, err := tx.SearchMemoryItems(ctx, scope, scopeID, item.Query, store.MemoryItemSearchOptions{
		Limit:          &limit,
		IncludeContent: &includeContent,
	}, nil)
	if err != nil {
		return batchResult{Index: index, Op: "search", Success: false, Error: err.Error()}
	}

	matches := []matchEntry{}
	for _, result := range results {
		entry := matchEntry{}
		if result.MemoryItemID != nil {
			entry.ID = *result.MemoryItemID
		}
		if result.Title != nil {
			entry.Title = *result.Title
		}
		if result.Tags != nil {
			entry.Tags = *result.Tags
		}
		if result.MatchedLines != nil && len(*result.MatchedLines) > 0 {
			entry.Snippet = strings.Join(*result.MatchedLines, "\n")
		}
		matches = append(matches, entry)
		if len(matches) >= maxResults {
			break
		}
	}
	return batchResult{Index: index, Op: "search", Success: true, Matches: matches}
}

func (self *memoryTool) callAfterMutate(ctx context.Context, scopeID string) {
	if self.configuration.afterMutate != nil {
		if err := self.configuration.afterMutate(ctx, scopeID); err != nil {
			log.Warningf("failed to call after mutate: %v", err)
		}
	}
}

func memoryItemToOutput(item *models.MemoryItem) map[string]interface{} {
	out := map[string]interface{}{
		"id": item.ID,
	}
	if item.Title != nil {
		out["title"] = *item.Title
	}
	if item.Content != nil {
		out["content"] = *item.Content
	}
	if item.Tags != nil {
		out["tags"] = *item.Tags
	}
	if item.CreatedAt != nil {
		out["createdAt"] = item.CreatedAt.Format(time.RFC3339)
	}
	if item.ModifiedAt != nil {
		out["modifiedAt"] = item.ModifiedAt.Format(time.RFC3339)
	}
	return out
}
