package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/embeddings"
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
			afterMutate: func(ctx context.Context, scopeId string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyAgent(ctx, scopeId, func(agent *models.Agent) error {
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
			afterMutate: func(ctx context.Context, scopeId string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyUser(ctx, scopeId, func(user *models.User) error {
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
			scopeIdParameterName:        "projectId",
			scopeIdParameterDescription: "Project ID for project memory operations.",
			resolveScope: func(_ context.Context, scopeId string) (models.Scope, string, error) {
				if scopeId == "" {
					return "", "", fmt.Errorf("projectId is required")
				}
				return models.ScopeProject, scopeId, nil
			},
			afterMutate: func(ctx context.Context, scopeId string) error {
				return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyProject(ctx, scopeId, func(project *models.Project) error {
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
	scopeIdParameterName        string
	scopeIdParameterDescription string
	resolveScope                func(ctx context.Context, scopeId string) (models.Scope, string, error)
	afterMutate                 func(ctx context.Context, scopeId string) error
}

type memoryTool struct {
	configuration memoryToolConfiguration
}

func newMemoryTool(configuration memoryToolConfiguration) *memoryTool {
	return &memoryTool{configuration: configuration}
}

type batchItem struct {
	Op      string   `json:"op"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

type batchResult struct {
	Index   int                      `json:"index"`
	Op      string                   `json:"op"`
	Success bool                     `json:"success"`
	Item    map[string]interface{}   `json:"item,omitempty"`
	Items   []map[string]interface{} `json:"items,omitempty"`
	Matches interface{}              `json:"matches,omitempty"`
	Error   string                   `json:"error,omitempty"`
	Warning string                   `json:"warning,omitempty"`
}

const dedupeThreshold = 0.90

type batchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

func (self *memoryTool) Definition() providers.ToolDefinition {
	batchOps := []string{"add", "update", "delete", "get"}
	properties := map[string]interface{}{
		"action": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"get", "list", "search", "batch", "retrieve", "summary", "filter"},
			"description": "The memory action to perform.",
		},
		// Single-action parameters.
		"id": map[string]interface{}{
			"type":        "string",
			"description": "Memory item ID (required for get).",
		},
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Search query string (required for search).",
		},
		"tags": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Tags filter (optional for list).",
		},
		"maxResults": map[string]interface{}{
			"type":        "integer",
			"description": "Maximum results to return, default 10 (list/search).",
		},
		// Retrieve parameters.
		"contextLines": map[string]interface{}{
			"type":        "integer",
			"description": "Lines of surrounding context per snippet (retrieve, default 1).",
		},
		// Summary / filter parameters.
		"roles": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Filter by message role (summary/filter).",
		},
		"keyword": map[string]interface{}{
			"type":        "string",
			"description": "Case-insensitive substring match on content (filter).",
		},
		"after": map[string]interface{}{
			"type":        "string",
			"description": "Include messages created after this ISO8601 time (filter).",
		},
		"before": map[string]interface{}{
			"type":        "string",
			"description": "Include messages created before this ISO8601 time (filter).",
		},
		"maxMessages": map[string]interface{}{
			"type":        "integer",
			"description": "Limit to last N messages (summary).",
		},
		"persist": map[string]interface{}{
			"type":        "object",
			"description": "If present, persist the result as a memory item (summary/filter).",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Title for the persisted memory item.",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tags for the persisted memory item.",
				},
			},
		},
		// Batch parameters.
		"items": map[string]interface{}{
			"type":        "array",
			"minItems":    1,
			"maxItems":    maxBatchItems,
			"description": "Array of operations to execute (1-50, required for batch). Each item op must be add, update, delete, or get.",
			"items": map[string]interface{}{
				"type":     "object",
				"required": []string{"op"},
				"properties": map[string]interface{}{
					"op": map[string]interface{}{
						"type":        "string",
						"enum":        batchOps,
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
				},
			},
		},
	}

	required := []string{"action"}

	if self.configuration.scopeIdParameterName != "" {
		properties[self.configuration.scopeIdParameterName] = map[string]interface{}{
			"type":        "string",
			"description": self.configuration.scopeIdParameterDescription,
		}
		required = append(required, self.configuration.scopeIdParameterName)
	}

	descriptionSuffix := " Actions: get (by id), list (optional tags/maxResults), search (by query), batch (items array of add/update/delete/get ops), retrieve (keyword-ranked snippets), summary (conversation summary), filter (filter conversation messages)."

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
				"description": "Single actions return {action, item/items/matches}. Batch returns {action, results:[...], summary:{total,succeeded,failed}}.",
			},
		},
	}
}

type persistOptions struct {
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
}

type executeArguments struct {
	Action       string          `json:"action"`
	ID           string          `json:"id"`
	Query        string          `json:"query"`
	Tags         []string        `json:"tags"`
	MaxResults   int             `json:"maxResults"`
	Items        []batchItem     `json:"items"`
	ScopeID      string          `json:"-"`
	ContextLines int             `json:"contextLines"`
	Roles        []string        `json:"roles"`
	Keyword      string          `json:"keyword"`
	After        string          `json:"after"`
	Before       string          `json:"before"`
	MaxMessages  int             `json:"maxMessages"`
	Persist      *persistOptions `json:"persist"`
}

func (self *memoryTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments executeArguments
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	if self.configuration.scopeIdParameterName != "" {
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal([]byte(rawArguments), &rawMap); err == nil {
			if raw, ok := rawMap[self.configuration.scopeIdParameterName]; ok {
				var scopeId string
				if err := json.Unmarshal(raw, &scopeId); err == nil {
					arguments.ScopeID = scopeId
				}
			}
		}
	}

	scope, scopeId, scopeError := self.configuration.resolveScope(ctx, arguments.ScopeID)
	if scopeError != nil {
		return "", scopeError
	}

	switch arguments.Action {
	case "get":
		return self.executeGet(ctx, scope, scopeId, arguments)
	case "list":
		return self.executeList(ctx, scope, scopeId, arguments)
	case "search":
		return self.executeSearch(ctx, scope, scopeId, arguments)
	case "batch":
		return self.executeBatch(ctx, scope, scopeId, arguments.Items)
	case "retrieve":
		return self.executeRetrieve(ctx, scope, scopeId, arguments)
	case "summary":
		return self.executeSummary(ctx, scope, scopeId, arguments)
	case "filter":
		return self.executeFilter(ctx, scope, scopeId, arguments)
	default:
		return "", fmt.Errorf("unknown action %q: must be get, list, search, batch, retrieve, summary, or filter", arguments.Action)
	}
}

func (self *memoryTool) executeGet(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	if args.ID == "" {
		return "", fmt.Errorf("id is required for get")
	}
	var item *models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		item, err = tx.GetMemoryItem(ctx, args.ID, nil)
		return err
	}); err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "get",
		"item":   memoryItemToOutput(item),
	})
	return string(output), nil
}

func (self *memoryTool) executeList(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	limit := uint64(maxResults)

	listOptions := store.MemoryItemListOptions{
		Limit: &limit,
	}
	if len(args.Tags) > 0 {
		listOptions.Tags = &args.Tags
	}

	var items []*models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		items, err = tx.ListMemoryItems(ctx, scope, scopeId, listOptions, nil)
		return err
	}); err != nil {
		return "", err
	}

	outputItems := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		entry := map[string]interface{}{
			"id": item.ID,
		}
		if item.Title != nil {
			entry["title"] = *item.Title
		}
		if item.Tags != nil {
			entry["tags"] = *item.Tags
		}
		if item.CreatedAt != nil {
			entry["createdAt"] = item.CreatedAt.Format(time.RFC3339)
		}
		if item.ModifiedAt != nil {
			entry["modifiedAt"] = item.ModifiedAt.Format(time.RFC3339)
		}
		outputItems = append(outputItems, entry)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"items":  outputItems,
	})
	return string(output), nil
}

func (self *memoryTool) executeSearch(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	if args.Query == "" {
		return "", fmt.Errorf("query is required for search")
	}
	maxResults := args.MaxResults
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

	var matches []matchEntry
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		results, err := tx.SearchMemoryItems(ctx, scope, scopeId, args.Query, store.MemoryItemSearchOptions{
			Limit:          &limit,
			IncludeContent: &includeContent,
		}, nil)
		if err != nil {
			return err
		}
		matches = make([]matchEntry, 0, len(results))
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
		return nil
	}); err != nil {
		return "", err
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":  "search",
		"matches": matches,
	})
	return string(output), nil
}

func (self *memoryTool) executeBatch(ctx context.Context, scope models.Scope, scopeId string, items []batchItem) (string, error) {
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
		for index, item := range items {
			results[index] = self.executeBatchItem(ctx, tx, scope, scopeId, index, item)
			if results[index].Success {
				succeeded++
			}
			if results[index].Success && isMutatingOp(item.Op) {
				anyMutation = true
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if anyMutation {
		self.callAfterMutate(ctx, scopeId)
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

func (self *memoryTool) executeBatchItem(ctx context.Context, tx store.Transaction, scope models.Scope, scopeId string, index int, item batchItem) batchResult {
	switch item.Op {
	case "add":
		return self.batchAdd(ctx, tx, scope, scopeId, index, item)
	case "update":
		return self.batchUpdate(ctx, tx, index, item)
	case "delete":
		return self.batchDelete(ctx, tx, index, item)
	case "get":
		return self.batchGet(ctx, tx, index, item)
	case "list", "search":
		return batchResult{Index: index, Op: item.Op, Success: false, Error: fmt.Sprintf("op %q is not allowed in batch; use it as a top-level action instead", item.Op)}
	default:
		return batchResult{Index: index, Op: item.Op, Success: false, Error: fmt.Sprintf("unknown op: %s", item.Op)}
	}
}

func (self *memoryTool) batchAdd(ctx context.Context, tx store.Transaction, scope models.Scope, scopeId string, index int, item batchItem) batchResult {
	if item.Content == "" {
		return batchResult{Index: index, Op: "add", Success: false, Error: "content is required for add"}
	}
	if len(item.Content) > maxContentSize {
		return batchResult{Index: index, Op: "add", Success: false, Error: fmt.Sprintf("content exceeds maximum size of %d bytes", maxContentSize)}
	}

	newItem := &models.MemoryItem{
		Scope:   &scope,
		ScopeID: &scopeId,
		Content: &item.Content,
	}
	if item.Title != "" {
		newItem.Title = &item.Title
	}
	if len(item.Tags) > 0 {
		newItem.Tags = &item.Tags
	}

	// Compute embedding if embedder is available.
	var newEmbedding []float64
	var warning string
	runner := runners.RunnerFromContext(ctx)
	if runner != nil && runner.Embedder != nil {
		embedder := runner.Embedder
		embeddingText := item.Content
		if item.Title != "" {
			embeddingText = item.Title + "\n" + item.Content
		}
		vector, embeddingModel, embedError := embedder.Embed(ctx, embeddingText)
		if embedError != nil {
			log.Warningf("embedding for new memory item failed: %v", embedError)
		} else {
			newEmbedding = vector
			now := time.Now()
			newItem.EmbeddingProviderModelName = &embeddingModel
			newItem.Embedding = &vector
			newItem.EmbeddedAt = &now
		}
	}

	// Dedupe check: compare new embedding against existing items in same scope.
	if newEmbedding != nil {
		existingItems, listError := tx.ListMemoryItems(ctx, scope, scopeId, store.MemoryItemListOptions{}, nil)
		if listError == nil {
			warning = checkDuplicates(newEmbedding, *newItem.EmbeddingProviderModelName, existingItems)
		}
	}

	created, err := tx.CreateMemoryItem(ctx, newItem, nil)
	if err != nil {
		return batchResult{Index: index, Op: "add", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "add", Success: true, Item: memoryItemToOutput(created), Warning: warning}
}

// checkDuplicates compares an embedding vector against existing memory items
// with the same provider model name and returns a warning string if any item
// exceeds the deduplication threshold.
func checkDuplicates(newEmbedding []float64, providerModelName string, existingItems []*models.MemoryItem) string {
	var maxSimilarity float64
	var mostSimilarID string
	var mostSimilarTitle string
	for _, existing := range existingItems {
		if existing.Embedding == nil || len(*existing.Embedding) == 0 {
			continue
		}
		if existing.EmbeddingProviderModelName == nil || *existing.EmbeddingProviderModelName != providerModelName {
			continue
		}
		similarity := embeddings.CosineSimilarity(newEmbedding, *existing.Embedding)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
			mostSimilarID = existing.ID
			if existing.Title != nil {
				mostSimilarTitle = *existing.Title
			}
		}
	}
	if maxSimilarity >= dedupeThreshold {
		titleHint := ""
		if mostSimilarTitle != "" {
			titleHint = fmt.Sprintf(" (%s)", mostSimilarTitle)
		}
		return fmt.Sprintf("possible duplicate: %.0f%% similar to item %s%s", maxSimilarity*100, mostSimilarID, titleHint)
	}
	return ""
}

func (self *memoryTool) batchUpdate(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.ID == "" {
		return batchResult{Index: index, Op: "update", Success: false, Error: "id is required for update"}
	}
	if item.Content != "" && len(item.Content) > maxContentSize {
		return batchResult{Index: index, Op: "update", Success: false, Error: fmt.Sprintf("content exceeds maximum size of %d bytes", maxContentSize)}
	}

	updated, err := tx.ModifyMemoryItem(ctx, item.ID, func(existing *models.MemoryItem) error {
		if item.Title != "" {
			existing.Title = &item.Title
		}
		if item.Content != "" {
			existing.Content = &item.Content
		}
		if item.Tags != nil {
			existing.Tags = &item.Tags
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
	memoryItem, err := tx.GetMemoryItem(ctx, item.ID, nil)
	if err != nil {
		return batchResult{Index: index, Op: "get", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "get", Success: true, Item: memoryItemToOutput(memoryItem)}
}

func (self *memoryTool) callAfterMutate(ctx context.Context, scopeId string) {
	if self.configuration.afterMutate != nil {
		if err := self.configuration.afterMutate(ctx, scopeId); err != nil {
			log.Warningf("failed to call after mutate: %v", err)
		}
	}
}

func memoryItemToOutput(item *models.MemoryItem) map[string]interface{} {
	output := map[string]interface{}{
		"id": item.ID,
	}
	if item.Title != nil {
		output["title"] = *item.Title
	}
	if item.Content != nil {
		output["content"] = *item.Content
	}
	if item.Tags != nil {
		output["tags"] = *item.Tags
	}
	if item.CreatedAt != nil {
		output["createdAt"] = item.CreatedAt.Format(time.RFC3339)
	}
	if item.ModifiedAt != nil {
		output["modifiedAt"] = item.ModifiedAt.Format(time.RFC3339)
	}
	return output
}
