package v1api

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

func (self *webSocketConnection) handleConversationsTodosList(frame requestFrame) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
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
		self.sendError(frame.ID, 500, "listing todos: "+err.Error())
		return
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

	self.sendResponse(frame.ID, map[string]interface{}{
		"todos":     todos,
		"openCount": openCount,
		"doneCount": doneCount,
	})
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

func (self *webSocketConnection) handleConversationsTodosBatch(frame requestFrame) {
	var parameters struct {
		ConversationID string         `json:"conversationId"`
		Items          []rpcBatchItem `json:"items"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}
	if len(parameters.Items) == 0 {
		self.sendError(frame.ID, 400, "items is required and must contain 1-50 entries")
		return
	}
	if len(parameters.Items) > 50 {
		self.sendError(frame.ID, 400, fmt.Sprintf("items must contain at most 50 entries, got %d", len(parameters.Items)))
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	results := make([]rpcBatchResult, len(parameters.Items))
	succeeded := 0
	anySuccess := false

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		for i, item := range parameters.Items {
			results[i] = self.executeRpcBatchItem(ctx, tx, parameters.ConversationID, i, item)
			if results[i].Success {
				succeeded++
				anySuccess = true
			}
		}
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "batch operation failed: "+err.Error())
		return
	}

	if anySuccess {
		// Update conversation modified timestamp.
		_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
			_, err := tx.ModifyConversation(ctx, parameters.ConversationID, func(c *models.Conversation) error {
				now := time.Now()
				c.ModifiedAt = &now
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

	self.sendResponse(frame.ID, map[string]interface{}{
		"results": results,
		"summary": map[string]interface{}{
			"total":     len(parameters.Items),
			"succeeded": succeeded,
			"failed":    len(parameters.Items) - succeeded,
		},
		"totalCount": len(todos),
		"openCount":  openCount,
		"doneCount":  doneCount,
	})
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

// --- helpers ---

func (self *webSocketConnection) verifyConversationAccess(conversationId string) error {
	var conversation *models.Conversation
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		conv, err := tx.GetConversation(ctx, conversationId, nil)
		if err != nil {
			return err
		}
		conversation = conv
		return nil
	}); err != nil {
		return err
	}
	userId := self.userId()
	if conversation.GetUserID() != userId && !self.isAdmin() {
		return store.ErrNotFound
	}
	return nil
}

