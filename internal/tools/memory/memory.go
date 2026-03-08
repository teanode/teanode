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

func (self *memoryTool) Definition() providers.ToolDefinition {
	actions := []string{"add", "update", "delete", "get", "list", "search"}
	properties := map[string]interface{}{
		"action": map[string]interface{}{
			"type":        "string",
			"enum":        actions,
			"description": "The memory action to perform.",
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
			"description": "Search query string (search action).",
		},
		"maxResults": map[string]interface{}{
			"type":        "integer",
			"description": "Maximum results to return, default 10 (list/search).",
		},
	}

	required := []string{"action"}

	if self.configuration.scopeIDParameterName != "" {
		properties[self.configuration.scopeIDParameterName] = map[string]interface{}{
			"type":        "string",
			"description": self.configuration.scopeIDParameterDescription,
		}
		required = append(required, self.configuration.scopeIDParameterName)
	}

	descriptionSuffix := " Actions: add (store a new memory), update (modify an existing memory), " +
		"delete (soft-delete a memory), get (retrieve a memory by ID), list (list memories), search (search memories by keyword)."

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
				"description": "Action-dependent result. add/update/get: {action, item}. delete: {action, success}. list: {action, items}. search: {action, matches}.",
			},
		},
	}
}

func (self *memoryTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string   `json:"action"`
		ID         string   `json:"id"`
		Title      string   `json:"title"`
		Content    string   `json:"content"`
		Tags       []string `json:"tags"`
		Query      string   `json:"query"`
		MaxResults int      `json:"maxResults"`
		ScopeID    string   `json:"-"`
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

	switch arguments.Action {
	case "add":
		result, err := self.executeAdd(ctx, scope, scopeID, arguments.Title, arguments.Content, arguments.Tags)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeID)
		return result, nil
	case "update":
		result, err := self.executeUpdate(ctx, arguments.ID, arguments.Title, arguments.Content, arguments.Tags)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeID)
		return result, nil
	case "delete":
		result, err := self.executeDelete(ctx, arguments.ID)
		if err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeID)
		return result, nil
	case "get":
		return self.executeGet(ctx, arguments.ID)
	case "list":
		return self.executeList(ctx, scope, scopeID, arguments.Tags, arguments.MaxResults)
	case "search":
		return self.executeSearch(ctx, scope, scopeID, arguments.Query, arguments.MaxResults)
	default:
		return "", fmt.Errorf("unknown memory action: %s", arguments.Action)
	}
}

func (self *memoryTool) callAfterMutate(ctx context.Context, scopeID string) {
	if self.configuration.afterMutate != nil {
		if err := self.configuration.afterMutate(ctx, scopeID); err != nil {
			log.Warningf("failed to call after mutate: %v", err)
		}
	}
}

func (self *memoryTool) executeAdd(
	ctx context.Context,
	scope models.Scope,
	scopeID string,
	title string,
	content string,
	tags []string,
) (string, error) {
	if content == "" {
		return "", fmt.Errorf("content is required for add action")
	}
	if len(content) > maxContentSize {
		return "", fmt.Errorf("content exceeds maximum size of %d bytes", maxContentSize)
	}

	var createdItem *models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		contentBytes := []byte(content)
		item := &models.MemoryItem{
			Scope:   &scope,
			ScopeID: &scopeID,
			Content: &contentBytes,
		}
		if title != "" {
			item.Title = &title
		}
		if len(tags) > 0 {
			item.Tags = &tags
		}
		var createError error
		createdItem, createError = transaction.CreateMemoryItem(ctx, item, nil)
		return createError
	}); err != nil {
		return "", fmt.Errorf("adding memory item: %w", err)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action": "add",
		"item":   memoryItemToOutput(createdItem),
	})
	return string(output), nil
}

func (self *memoryTool) executeUpdate(
	ctx context.Context,
	id string,
	title string,
	content string,
	tags []string,
) (string, error) {
	if id == "" {
		return "", fmt.Errorf("id is required for update action")
	}
	if content != "" && len(content) > maxContentSize {
		return "", fmt.Errorf("content exceeds maximum size of %d bytes", maxContentSize)
	}

	var updatedItem *models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var modifyError error
		updatedItem, modifyError = transaction.ModifyMemoryItem(ctx, id, func(item *models.MemoryItem) error {
			if title != "" {
				item.Title = &title
			}
			if content != "" {
				contentBytes := []byte(content)
				item.Content = &contentBytes
			}
			if tags != nil {
				item.Tags = &tags
			}
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		return "", fmt.Errorf("updating memory item: %w", err)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action": "update",
		"item":   memoryItemToOutput(updatedItem),
	})
	return string(output), nil
}

func (self *memoryTool) executeDelete(
	ctx context.Context,
	id string,
) (string, error) {
	if id == "" {
		return "", fmt.Errorf("id is required for delete action")
	}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteMemoryItem(ctx, id, nil)
	}); err != nil {
		return "", fmt.Errorf("deleting memory item: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action":  "delete",
		"success": true,
	})
	return string(output), nil
}

func (self *memoryTool) executeGet(
	ctx context.Context,
	id string,
) (string, error) {
	if id == "" {
		return "", fmt.Errorf("id is required for get action")
	}
	var item *models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var getError error
		item, getError = transaction.GetMemoryItem(ctx, id, nil)
		return getError
	}); err != nil {
		return "", fmt.Errorf("getting memory item: %w", err)
	}
	output, _ := json.Marshal(map[string]interface{}{
		"action": "get",
		"item":   memoryItemToOutput(item),
	})
	return string(output), nil
}

func (self *memoryTool) executeList(
	ctx context.Context,
	scope models.Scope,
	scopeID string,
	tags []string,
	maxResults int,
) (string, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	limit := uint64(maxResults)

	var items []*models.MemoryItem
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		listOptions := store.MemoryItemListOptions{
			Limit: &limit,
		}
		if len(tags) > 0 {
			listOptions.Tags = &tags
		}
		var listError error
		items, listError = transaction.ListMemoryItems(ctx, scope, scopeID, listOptions, nil)
		return listError
	}); err != nil {
		return "", fmt.Errorf("listing memory items: %w", err)
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

func (self *memoryTool) executeSearch(
	ctx context.Context,
	scope models.Scope,
	scopeID string,
	query string,
	maxResults int,
) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for search action")
	}
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

	matches := []matchEntry{}
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		results, err := transaction.SearchMemoryItems(ctx, scope, scopeID, query, store.MemoryItemSearchOptions{
			Limit:          &limit,
			IncludeContent: &includeContent,
		}, nil)
		if err != nil {
			return err
		}
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
		return "", fmt.Errorf("searching memory items: %w", err)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"action":  "search",
		"matches": matches,
	})
	return string(output), nil
}

func memoryItemToOutput(item *models.MemoryItem) map[string]interface{} {
	out := map[string]interface{}{
		"id": item.ID,
	}
	if item.Title != nil {
		out["title"] = *item.Title
	}
	if item.Content != nil {
		out["content"] = string(*item.Content)
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
