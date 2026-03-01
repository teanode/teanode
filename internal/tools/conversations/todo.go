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

type conversationTodoResponse struct {
	Action     string         `json:"action"`
	Todo       *models.Todo   `json:"todo,omitempty"`
	Todos      []*models.Todo `json:"todos,omitempty"`
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
			Description: "Manage conversation-scoped todos/tasks. Actions: list, add, update, complete, reopen, delete, clear_done, reset. Todos are private to the conversation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "add", "update", "complete", "reopen", "delete", "clear_done", "reset"},
						"description": "The todo action to perform.",
					},
					"conversationId": map[string]interface{}{
						"type":        "string",
						"description": "Conversation ID. If omitted, uses the current conversation.",
					},
					"todoId": map[string]interface{}{
						"type":        "string",
						"description": "Todo ID (for update, complete, reopen, delete).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Todo title (for add, update).",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Optional longer description (for add, update).",
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"low", "medium", "high"},
						"description": "Priority level (for add, update, list filter).",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Labels/tags (for add, update).",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter by tag (for list).",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "done"},
						"description": "Filter by status (for list). Default: returns all.",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *conversationTodoTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action         string   `json:"action"`
		ConversationID string   `json:"conversationId"`
		TodoID         string   `json:"todoId"`
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		Priority       string   `json:"priority"`
		Tags           []string `json:"tags"`
		Tag            string   `json:"tag"`
		Status         string   `json:"status"`
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

	action := arguments.Action
	switch action {
	case "list":
		return self.executeList(ctx, conversationId, arguments.Status, arguments.Priority, arguments.Tag)
	case "add":
		return self.executeAdd(ctx, conversationId, user.ID, arguments.Title, arguments.Description, arguments.Priority, arguments.Tags)
	case "update":
		return self.executeUpdate(ctx, conversationId, user.ID, arguments.TodoID, arguments.Title, arguments.Description, arguments.Priority, arguments.Tags)
	case "complete":
		return self.executeComplete(ctx, conversationId, user.ID, arguments.TodoID)
	case "reopen":
		return self.executeReopen(ctx, conversationId, user.ID, arguments.TodoID)
	case "delete":
		return self.executeDelete(ctx, conversationId, user.ID, arguments.TodoID)
	case "clear_done":
		return self.executeClearDone(ctx, conversationId, user.ID)
	case "reset":
		return self.executeReset(ctx, conversationId, user.ID)
	default:
		return "", fmt.Errorf("unknown conversation_todo action: %s", action)
	}
}

func (self *conversationTodoTool) executeList(ctx context.Context, conversationId, statusFilter, priorityFilter, tagFilter string) (string, error) {
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

	filtered := filterTodos(todos, statusFilter, priorityFilter, tagFilter)
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

func (self *conversationTodoTool) executeAdd(ctx context.Context, conversationId, userId, title, description, priority string, tags []string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	if priority == "" {
		priority = string(models.TodoPriorityMedium)
	}
	if tags == nil {
		tags = make([]string, 0)
	}

	var created *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todo := &models.Todo{
			ID:             security.NewULID(),
			ConversationID: ptrto.Value(conversationId),
			Title:          ptrto.Value(title),
			Status:         ptrto.Value(models.TodoStatusOpen),
			Priority:       ptrto.Value(models.TodoPriority(priority)),
			Tags:           &tags,
		}
		if description != "" {
			todo.Description = ptrto.Value(description)
		}
		result, err := tx.CreateTodo(ctx, todo, nil)
		if err != nil {
			return err
		}
		created = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateConversation(ctx, conversationId)
	emitTodoEvent(ctx, conversationId, userId, created, "add")
	output, _ := json.Marshal(conversationTodoResponse{Action: "add", Todo: created})
	return string(output), nil
}

func (self *conversationTodoTool) executeUpdate(ctx context.Context, conversationId, userId, todoId, title, description, priority string, tags []string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	var updated *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, todoId, func(todo *models.Todo) error {
			if title != "" {
				todo.Title = ptrto.Value(title)
			}
			if description != "" {
				todo.Description = ptrto.Value(description)
			}
			if priority != "" {
				todo.Priority = ptrto.Value(models.TodoPriority(priority))
			}
			if tags != nil {
				todo.Tags = &tags
			}
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateConversation(ctx, conversationId)
	emitTodoEvent(ctx, conversationId, userId, updated, "update")
	output, _ := json.Marshal(conversationTodoResponse{Action: "update", Todo: updated})
	return string(output), nil
}

func (self *conversationTodoTool) executeComplete(ctx context.Context, conversationId, userId, todoId string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	var updated *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, todoId, func(todo *models.Todo) error {
			todo.Status = ptrto.Value(models.TodoStatusDone)
			now := time.Now()
			todo.CompletedAt = &now
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateConversation(ctx, conversationId)
	emitTodoEvent(ctx, conversationId, userId, updated, "complete")
	output, _ := json.Marshal(conversationTodoResponse{Action: "complete", Todo: updated})
	return string(output), nil
}

func (self *conversationTodoTool) executeReopen(ctx context.Context, conversationId, userId, todoId string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	var updated *models.Todo
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, todoId, func(todo *models.Todo) error {
			todo.Status = ptrto.Value(models.TodoStatusOpen)
			todo.CompletedAt = nil
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		return "", err
	}

	afterMutateConversation(ctx, conversationId)
	emitTodoEvent(ctx, conversationId, userId, updated, "reopen")
	output, _ := json.Marshal(conversationTodoResponse{Action: "reopen", Todo: updated})
	return string(output), nil
}

func (self *conversationTodoTool) executeDelete(ctx context.Context, conversationId, userId, todoId string) (string, error) {
	if todoId == "" {
		return "", fmt.Errorf("todoId is required")
	}

	// Get the todo before deleting so we can emit event with the ID.
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteTodo(ctx, todoId, nil)
	}); err != nil {
		return "", err
	}

	afterMutateConversation(ctx, conversationId)
	emitTodoEvent(ctx, conversationId, userId, &models.Todo{ID: todoId}, "delete")
	output, _ := json.Marshal(conversationTodoResponse{Action: "delete", Success: true})
	return string(output), nil
}

func (self *conversationTodoTool) executeClearDone(ctx context.Context, conversationId, userId string) (string, error) {
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
		emitTodoEvent(ctx, conversationId, userId, &models.Todo{}, "clear_done")
	}
	output, _ := json.Marshal(conversationTodoResponse{Action: "clear_done", Success: true, DoneCount: deleted})
	return string(output), nil
}

func (self *conversationTodoTool) executeReset(ctx context.Context, conversationId, userId string) (string, error) {
	var deleted int
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		todos, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &conversationId}, nil)
		if err != nil {
			return err
		}
		for _, todo := range todos {
			if err := tx.DeleteTodo(ctx, todo.ID, nil); err != nil {
				return err
			}
			deleted++
		}
		return nil
	}); err != nil {
		return "", err
	}

	if deleted > 0 {
		afterMutateConversation(ctx, conversationId)
		emitTodoEvent(ctx, conversationId, userId, &models.Todo{}, "reset")
	}
	output, _ := json.Marshal(conversationTodoResponse{Action: "reset", Success: true, TotalCount: deleted})
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
