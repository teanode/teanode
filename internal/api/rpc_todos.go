package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

// --- conversations.todos.list ---

func (self *webSocketConnection) handleConversationsTodosList(frame requestFrame) (interface{}, error) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		return nil, rpcError(403, err.Error())
	}

	var todos []*models.Todo
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &parameters.ConversationID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing todos: "+err.Error())
	}

	openCount, doneCount := 0, 0
	for _, todo := range todos {
		switch todo.GetStatus() {
		case models.TodoStatusOpen:
			openCount++
		case models.TodoStatusDone:
			doneCount++
		}
	}

	return map[string]interface{}{
		"todos":     todos,
		"openCount": openCount,
		"doneCount": doneCount,
	}, nil
}

// --- conversations.todos.batch ---

type rpcBatchItem struct {
	Op          string   `json:"op"`
	TodoID      string   `json:"todoId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Tags        []string `json:"tags"`
}

type rpcBatchResult struct {
	Index   int          `json:"index"`
	Op      string       `json:"op"`
	Success bool         `json:"success"`
	Todo    *models.Todo `json:"todo,omitempty"`
	Error   string       `json:"error,omitempty"`
	TodoID  string       `json:"todoId,omitempty"`
}

func (self *webSocketConnection) handleConversationsTodosBatch(frame requestFrame) (interface{}, error) {
	var parameters struct {
		ConversationID string         `json:"conversationId"`
		Items          []rpcBatchItem `json:"items"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}
	if len(parameters.Items) == 0 {
		return nil, rpcError(400, "items is required and must contain 1-50 entries")
	}
	if len(parameters.Items) > 50 {
		return nil, rpcError(400, fmt.Sprintf("items must contain at most 50 entries, got %d", len(parameters.Items)))
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		return nil, rpcError(403, err.Error())
	}

	results := make([]rpcBatchResult, len(parameters.Items))
	succeeded := 0
	anySuccess := false

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		for itemIndex, item := range parameters.Items {
			results[itemIndex] = self.executeRpcBatchItem(ctx, tx, parameters.ConversationID, itemIndex, item)
			if results[itemIndex].Success {
				succeeded++
				anySuccess = true
			}
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "batch operation failed: "+err.Error())
	}

	if anySuccess {
		// Update conversation modified timestamp.
		_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
			_, err := tx.ModifyConversation(ctx, parameters.ConversationID, func(conversation *models.Conversation) error {
				now := time.Now()
				conversation.ModifiedAt = &now
				return nil
			}, nil)
			return err
		})
	}

	// Compute aggregate counts.
	var todos []*models.Todo
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ConversationID: &parameters.ConversationID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	})
	openCount, doneCount := 0, 0
	for _, todo := range todos {
		switch todo.GetStatus() {
		case models.TodoStatusOpen:
			openCount++
		case models.TodoStatusDone:
			doneCount++
		}
	}

	// Emit single pubsub event.
	self.api.pubsub.Broadcast(pubsub.EventTypeConversationTodos, map[string]interface{}{
		"conversationId": parameters.ConversationID,
		"userId":         self.userId(),
		"action":         "batch",
		"results":        results,
	})

	return map[string]interface{}{
		"results": results,
		"summary": map[string]interface{}{
			"total":     len(parameters.Items),
			"succeeded": succeeded,
			"failed":    len(parameters.Items) - succeeded,
		},
		"totalCount": len(todos),
		"openCount":  openCount,
		"doneCount":  doneCount,
	}, nil
}

func (self *webSocketConnection) executeRpcBatchItem(ctx context.Context, tx store.Transaction, conversationId string, index int, item rpcBatchItem) rpcBatchResult {
	switch item.Op {
	case "add":
		return self.rpcBatchAdd(ctx, tx, conversationId, index, item)
	case "update":
		return self.rpcBatchUpdate(ctx, tx, index, item)
	case "complete":
		return self.rpcBatchComplete(ctx, tx, index, item)
	case "reopen":
		return self.rpcBatchReopen(ctx, tx, index, item)
	case "delete":
		return self.rpcBatchDelete(ctx, tx, index, item)
	default:
		return rpcBatchResult{Index: index, Op: item.Op, Success: false, Error: fmt.Sprintf("unknown op: %s", item.Op)}
	}
}

func (self *webSocketConnection) rpcBatchAdd(ctx context.Context, tx store.Transaction, conversationId string, index int, item rpcBatchItem) rpcBatchResult {
	if item.Title == "" {
		return rpcBatchResult{Index: index, Op: "add", Success: false, Error: "title is required for add"}
	}
	priority := models.TodoPriority(item.Priority)
	if priority == "" {
		priority = models.TodoPriorityMedium
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
		Priority:       ptrto.Value(priority),
		Tags:           &tags,
	}
	if item.Description != "" {
		todo.Description = ptrto.Value(item.Description)
	}
	created, err := tx.CreateTodo(ctx, todo, nil)
	if err != nil {
		return rpcBatchResult{Index: index, Op: "add", Success: false, Error: err.Error()}
	}
	return rpcBatchResult{Index: index, Op: "add", Success: true, Todo: created}
}

func (self *webSocketConnection) rpcBatchUpdate(ctx context.Context, tx store.Transaction, index int, item rpcBatchItem) rpcBatchResult {
	if item.TodoID == "" {
		return rpcBatchResult{Index: index, Op: "update", Success: false, Error: "todoId is required for update"}
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
		return rpcBatchResult{Index: index, Op: "update", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return rpcBatchResult{Index: index, Op: "update", Success: true, Todo: updated}
}

func (self *webSocketConnection) rpcBatchComplete(ctx context.Context, tx store.Transaction, index int, item rpcBatchItem) rpcBatchResult {
	if item.TodoID == "" {
		return rpcBatchResult{Index: index, Op: "complete", Success: false, Error: "todoId is required for complete"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		todo.Status = ptrto.Value(models.TodoStatusDone)
		now := time.Now()
		todo.CompletedAt = &now
		return nil
	}, nil)
	if err != nil {
		return rpcBatchResult{Index: index, Op: "complete", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return rpcBatchResult{Index: index, Op: "complete", Success: true, Todo: updated}
}

func (self *webSocketConnection) rpcBatchReopen(ctx context.Context, tx store.Transaction, index int, item rpcBatchItem) rpcBatchResult {
	if item.TodoID == "" {
		return rpcBatchResult{Index: index, Op: "reopen", Success: false, Error: "todoId is required for reopen"}
	}
	updated, err := tx.ModifyTodo(ctx, item.TodoID, func(todo *models.Todo) error {
		todo.Status = ptrto.Value(models.TodoStatusOpen)
		todo.CompletedAt = nil
		return nil
	}, nil)
	if err != nil {
		return rpcBatchResult{Index: index, Op: "reopen", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return rpcBatchResult{Index: index, Op: "reopen", Success: true, Todo: updated}
}

func (self *webSocketConnection) rpcBatchDelete(ctx context.Context, tx store.Transaction, index int, item rpcBatchItem) rpcBatchResult {
	if item.TodoID == "" {
		return rpcBatchResult{Index: index, Op: "delete", Success: false, Error: "todoId is required for delete"}
	}
	if err := tx.DeleteTodo(ctx, item.TodoID, nil); err != nil {
		return rpcBatchResult{Index: index, Op: "delete", Success: false, Error: err.Error(), TodoID: item.TodoID}
	}
	return rpcBatchResult{Index: index, Op: "delete", Success: true, TodoID: item.TodoID}
}

// --- projects.todos.summary ---

func (self *webSocketConnection) handleProjectsTodosSummary(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	type projectTodoSummary struct {
		ProjectID string `json:"projectId"`
		OpenCount int    `json:"openCount"`
		DoneCount int    `json:"doneCount"`
	}

	var projects []*models.Project
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		listed, err := tx.ListProjects(ctx, nil)
		if err != nil {
			return err
		}
		projects = listed
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing projects: "+err.Error())
	}

	summaries := make([]projectTodoSummary, 0, len(projects))
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		for _, project := range projects {
			todos, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &project.ID}, nil)
			if err != nil {
				return err
			}
			openCount, doneCount := 0, 0
			for _, todo := range todos {
				switch todo.GetStatus() {
				case models.TodoStatusOpen:
					openCount++
				case models.TodoStatusDone:
					doneCount++
				}
			}
			summaries = append(summaries, projectTodoSummary{
				ProjectID: project.ID,
				OpenCount: openCount,
				DoneCount: doneCount,
			})
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "counting todos: "+err.Error())
	}

	return map[string]interface{}{
		"summaries": summaries,
	}, nil
}

// --- projects.todos.list ---

func (self *webSocketConnection) handleProjectsTodosList(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}

	var parameters struct {
		ProjectID string `json:"projectId"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}
	if parameters.ProjectID == "" {
		return nil, rpcError(400, "projectId is required")
	}

	var todos []*models.Todo
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		// Verify project exists.
		if _, err := tx.GetProject(ctx, parameters.ProjectID, nil); err != nil {
			return err
		}
		listed, err := tx.ListTodos(ctx, store.TodoListOptions{ProjectID: &parameters.ProjectID}, nil)
		if err != nil {
			return err
		}
		todos = listed
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing project todos: "+err.Error())
	}

	openCount, doneCount := 0, 0
	for _, todo := range todos {
		switch todo.GetStatus() {
		case models.TodoStatusOpen:
			openCount++
		case models.TodoStatusDone:
			doneCount++
		}
	}

	return map[string]interface{}{
		"todos":     todos,
		"openCount": openCount,
		"doneCount": doneCount,
	}, nil
}
