package conversations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{NewConversationTodoTool()}
	})
}

func NewConversationTodoTool() *conversationTodoTool { return &conversationTodoTool{} }

type conversationTodoTool struct{}

type batchItem struct {
	Op          string   `json:"op"`
	TodoID      string   `json:"todoId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Tags        []string `json:"tags"`
}

type batchResult struct {
	Index   int          `json:"index"`
	Op      string       `json:"op"`
	Success bool         `json:"success"`
	Todo    *models.Todo `json:"todo,omitempty"`
	Error   string       `json:"error,omitempty"`
	TodoID  string       `json:"todoId,omitempty"`
}

type batchSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type conversationTodoResponse struct {
	Action     string         `json:"action"`
	Todo       *models.Todo   `json:"todo,omitempty"`
	Todos      []*models.Todo `json:"todos,omitempty"`
	Results    []batchResult  `json:"results,omitempty"`
	Summary    *batchSummary  `json:"summary,omitempty"`
	TotalCount int            `json:"totalCount,omitempty"`
	OpenCount  int            `json:"openCount,omitempty"`
	DoneCount  int            `json:"doneCount,omitempty"`
	Success    bool           `json:"success,omitempty"`
}

func (self *conversationTodoTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "conversation_todo",
			Description: "Manage conversation-scoped todos/tasks. Use 'list' to view, 'batch' to create/update/complete/reopen/delete one or more todos in a single call, 'prune' to remove all completed todos.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "batch", "prune"},
						"description": "The todo action to perform.",
					},
					"conversationId": map[string]interface{}{
						"type":        "string",
						"description": "Conversation ID. If omitted, uses the current conversation.",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "done"},
						"description": "Filter by status (for list). Default: returns all.",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter by tag (for list).",
					},
					"items": map[string]interface{}{
						"type":        "array",
						"minItems":    1,
						"maxItems":    50,
						"description": "Required when action is 'batch'. Each element describes one operation.",
						"items": map[string]interface{}{
							"type":     "object",
							"required": []string{"op"},
							"properties": map[string]interface{}{
								"op": map[string]interface{}{
									"type": "string",
									"enum": []string{"add", "update", "complete", "reopen", "delete"},
								},
								"todoId": map[string]interface{}{
									"type": "string",
								},
								"title": map[string]interface{}{
									"type":      "string",
									"maxLength": 512,
								},
								"description": map[string]interface{}{
									"type": "string",
								},
								"priority": map[string]interface{}{
									"type": "string",
									"enum": []string{"low", "medium", "high"},
								},
								"tags": map[string]interface{}{
									"type":  "array",
									"items": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *conversationTodoTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action         string      `json:"action"`
		ConversationID string      `json:"conversationId"`
		Status         string      `json:"status"`
		Tag            string      `json:"tag"`
		Items          []batchItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	// Resolve conversation ID from arguments or runner context.
	conversationId := arguments.ConversationID
	if conversationId == "" {
		runner := runners.RunnerFromContext(ctx)
		if runner != nil {
			conversationId = runner.ConversationID
		}
	}
	if conversationId == "" {
		return "", fmt.Errorf("conversationId is required")
	}

	// Verify ownership: the requesting user must own the conversation or be admin.
	user := models.UserFromContext(ctx)
	if user == nil {
		return "", fmt.Errorf("authentication required")
	}

	var conversation *models.Conversation
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		conv, err := tx.GetConversation(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		conversation = conv
		return nil
	}); err != nil {
		return "", fmt.Errorf("conversation not found: %s", conversationId)
	}

	if conversation.GetUserID() != user.ID && !user.GetAdmin() {
		return "", fmt.Errorf("access denied: conversation belongs to another user")
	}

	switch arguments.Action {
	case "list":
		return self.executeList(ctx, conversationId, arguments.Status, arguments.Tag)
	case "batch":
		return self.executeBatch(ctx, conversationId, user.ID, arguments.Items)
	case "prune":
		return self.executePrune(ctx, conversationId, user.ID)
	default:
		return "", fmt.Errorf("unknown conversation_todo action: %s", arguments.Action)
	}
}

func (self *conversationTodoTool) executeList(ctx context.Context, conversationId, statusFilter, tagFilter string) (string, error) {
	var todos []*models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &conversationId}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		return "", err
	}

	filtered := filterTodos(todos, statusFilter, "", tagFilter)
	openCount, doneCount := countByStatus(todos)

	output, _ := json.Marshal(conversationTodoResponse{
		Action:     "list",
		Todos:      filtered,
		TotalCount: len(todos),
		OpenCount:  openCount,
		DoneCount:  doneCount,
	})
	return string(output), nil
}

func (self *conversationTodoTool) executeBatch(ctx context.Context, conversationId, userId string, items []batchItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("items is required and must contain 1-50 entries")
	}
	if len(items) > 50 {
		return "", fmt.Errorf("items must contain at most 50 entries, got %d", len(items))
	}

	results := make([]batchResult, len(items))
	succeeded := 0
	anySuccess := false

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		for i, item := range items {
			results[i] = self.executeBatchItem(ctx, tx, conversationId, i, item)
			if results[i].Success {
				succeeded++
				anySuccess = true
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if anySuccess {
		afterMutateConversation(ctx, conversationId)
	}

	// Compute aggregate counts.
	var todos []*models.Todo
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &conversationId}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	})
	openCount, doneCount := countByStatus(todos)

	// Emit single pubsub event.
	emitTodoBatchEvent(ctx, conversationId, userId, results)

	output, _ := json.Marshal(conversationTodoResponse{
		Action:  "batch",
		Results: results,
		Summary: &batchSummary{
			Total:     len(items),
			Succeeded: succeeded,
			Failed:    len(items) - succeeded,
		},
		TotalCount: len(todos),
		OpenCount:  openCount,
		DoneCount:  doneCount,
	})
	return string(output), nil
}

func (self *conversationTodoTool) executeBatchItem(ctx context.Context, tx store.Transaction, conversationId string, index int, item batchItem) batchResult {
	switch item.Op {
	case "add":
		return self.batchAdd(ctx, tx, conversationId, index, item)
	case "update":
		return self.batchUpdate(ctx, tx, index, item)
	case "complete":
		return self.batchComplete(ctx, tx, index, item)
	case "reopen":
		return self.batchReopen(ctx, tx, index, item)
	case "delete":
		return self.batchDelete(ctx, tx, index, item)
	default:
		return batchResult{Index: index, Op: item.Op, Success: false, Error: fmt.Sprintf("unknown op: %s", item.Op)}
	}
}

func (self *conversationTodoTool) batchAdd(ctx context.Context, tx store.Transaction, conversationId string, index int, item batchItem) batchResult {
	if item.Title == "" {
		return batchResult{Index: index, Op: "add", Success: false, Error: "title is required for add"}
	}
	priority := item.Priority
	if priority == "" {
		priority = string(models.TodoPriorityMedium)
	}
	tags := item.Tags
	if tags == nil {
		tags = make([]string, 0)
	}
	todo := &models.Todo{
		ID:             security.NewULID(),
		ConversationID: ptrto.Value(conversationId),
		Title:          ptrto.Value(item.Title),
		Status:         ptrto.Value(models.TodoStatusOpen),
		Priority:       ptrto.Value(models.TodoPriority(priority)),
		Tags:           &tags,
	}
	if item.Description != "" {
		todo.Description = ptrto.Value(item.Description)
	}
	created, err := tx.CreateTodo(ctx, todo, nil)
	if err != nil {
		return batchResult{Index: index, Op: "add", Success: false, Error: err.Error()}
	}
	return batchResult{Index: index, Op: "add", Success: true, Todo: created}
}

func (self *conversationTodoTool) batchUpdate(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.TodoID == "" {
		return batchResult{Index: index, Op: "update", Success: false, Error: "todoId is required for update"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		if item.Title != "" {
			todo.Title = ptrto.Value(item.Title)
		}
		if item.Description != "" {
			todo.Description = ptrto.Value(item.Description)
		}
		if item.Priority != "" {
			todo.Priority = ptrto.Value(models.TodoPriority(item.Priority))
		}
		if item.Tags != nil {
			todo.Tags = &item.Tags
		}
		return nil
	}, nil)
	if err != nil {
		return batchResult{Index: index, Op: "update", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return batchResult{Index: index, Op: "update", Success: true, Todo: updated}
}

func (self *conversationTodoTool) batchComplete(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.TodoID == "" {
		return batchResult{Index: index, Op: "complete", Success: false, Error: "todoId is required for complete"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		todo.Status = ptrto.Value(models.TodoStatusDone)
		now := time.Now()
		todo.CompletedAt = &now
		return nil
	}, nil)
	if err != nil {
		return batchResult{Index: index, Op: "complete", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return batchResult{Index: index, Op: "complete", Success: true, Todo: updated}
}

func (self *conversationTodoTool) batchReopen(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.TodoID == "" {
		return batchResult{Index: index, Op: "reopen", Success: false, Error: "todoId is required for reopen"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		todo.Status = ptrto.Value(models.TodoStatusOpen)
		todo.CompletedAt = nil
		return nil
	}, nil)
	if err != nil {
		return batchResult{Index: index, Op: "reopen", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return batchResult{Index: index, Op: "reopen", Success: true, Todo: updated}
}

func (self *conversationTodoTool) batchDelete(ctx context.Context, tx store.Transaction, index int, item batchItem) batchResult {
	if item.TodoID == "" {
		return batchResult{Index: index, Op: "delete", Success: false, Error: "todoId is required for delete"}
	}
	if err := tx.DeleteTodo(ctx, item.TodoID, nil); err != nil {
		return batchResult{Index: index, Op: "delete", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return batchResult{Index: index, Op: "delete", Success: true, TodoID: item.TodoID}
}

func (self *conversationTodoTool) executePrune(ctx context.Context, conversationId, userId string) (string, error) {
	var deleted int
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &conversationId}, nil)
		if err != nil {
			return err
		}
		for _, todo := range todos {
			if todo.GetStatus() == models.TodoStatusDone {
				if err := tx.DeleteTodo(ctx, todo.ID, nil); err != nil {
					return err
				}
				deleted++
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	if deleted > 0 {
		afterMutateConversation(ctx, conversationId)
		emitTodoEvent(ctx, conversationId, userId, &models.Todo{}, "prune")
	}
	output, _ := json.Marshal(conversationTodoResponse{Action: "prune", Success: true, DoneCount: deleted})
	return string(output), nil
}

func afterMutateConversation(ctx context.Context, conversationId string) {
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConversation(ctx, conversationId, func(c *models.Conversation) error {
			now := time.Now()
			c.ModifiedAt = &now
			return nil
		}, nil)
		return err
	})
}

func emitTodoEvent(ctx context.Context, conversationId, userId string, todo *models.Todo, action string) {
	ps := pubsub.PubSubFromContext(ctx)
	if ps == nil {
		return
	}
	payload := map[string]interface{}{
		"conversationId": conversationId,
		"userId":         userId,
		"action":         action,
		"todoId":         todo.ID,
	}
	if action != "delete" {
		payload["todo"] = todo
	}
	ps.Broadcast(pubsub.EventTypeConversationTodos, payload)
}

func emitTodoBatchEvent(ctx context.Context, conversationId, userId string, results []batchResult) {
	ps := pubsub.PubSubFromContext(ctx)
	if ps == nil {
		return
	}
	ps.Broadcast(pubsub.EventTypeConversationTodos, map[string]interface{}{
		"conversationId": conversationId,
		"userId":         userId,
		"action":         "batch",
		"results":        results,
	})
}

func filterTodos(todos []*models.Todo, status, priority, tag string) []*models.Todo {
	if status == "" && priority == "" && tag == "" {
		return todos
	}
	filtered := make([]*models.Todo, 0, len(todos))
	for _, todo := range todos {
		if status != "" && todo.GetStatus() != models.TodoStatus(status) {
			continue
		}
		if priority != "" && todo.GetPriority() != models.TodoPriority(priority) {
			continue
		}
		if tag != "" && !containsTag(todo.GetTags(), tag) {
			continue
		}
		filtered = append(filtered, todo)
	}
	return filtered
}

func containsTag(tags []string, tag string) bool {
	lowerTag := strings.ToLower(tag)
	for _, t := range tags {
		if strings.ToLower(t) == lowerTag {
			return true
		}
	}
	return false
}

func countByStatus(todos []*models.Todo) (openCount, doneCount int) {
	for _, todo := range todos {
		switch todo.GetStatus() {
		case models.TodoStatusOpen:
			openCount++
		case models.TodoStatusDone:
			doneCount++
		}
	}
	return
}
