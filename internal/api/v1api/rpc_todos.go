package v1api

import (
	"context"
	"encoding/json"
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

// --- conversations.todos.add ---

func (self *webSocketConnection) handleConversationsTodosAdd(frame requestFrame) {
	var parameters struct {
		ConversationID string   `json:"conversationId"`
		Title          string   `json:"title"`
		Priority       string   `json:"priority"`
		Tags           []string `json:"tags"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}
	if parameters.Title == "" {
		self.sendError(frame.ID, 400, "title is required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	priority := models.TodoPriority(parameters.Priority)
	if priority == "" {
		priority = models.TodoPriorityMedium
	}
	tags := parameters.Tags
	if tags == nil {
		tags = make([]string, 0)
	}

	var created *models.Todo
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		todo := &models.Todo{
			ID:             security.NewULID(),
			ConversationID: ptrto.Value(parameters.ConversationID),
			Title:          ptrto.Value(parameters.Title),
			Status:         ptrto.Value(models.TodoStatusOpen),
			Priority:       ptrto.Value(priority),
			Tags:           &tags,
		}
		result, err := tx.CreateTodo(ctx, todo, nil)
		if err != nil {
			return err
		}
		created = result
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "creating todo: "+err.Error())
		return
	}

	self.emitConversationTodoEvent(parameters.ConversationID, created, "add")
	self.sendResponse(frame.ID, map[string]interface{}{"todo": created})
}

// --- conversations.todos.complete ---

func (self *webSocketConnection) handleConversationsTodosComplete(frame requestFrame) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
		TodoID         string `json:"todoId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" || parameters.TodoID == "" {
		self.sendError(frame.ID, 400, "conversationId and todoId are required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	var updated *models.Todo
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, parameters.TodoID, func(todo *models.Todo) error {
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
		self.sendError(frame.ID, 500, "completing todo: "+err.Error())
		return
	}

	self.emitConversationTodoEvent(parameters.ConversationID, updated, "complete")
	self.sendResponse(frame.ID, map[string]interface{}{"todo": updated})
}

// --- conversations.todos.reopen ---

func (self *webSocketConnection) handleConversationsTodosReopen(frame requestFrame) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
		TodoID         string `json:"todoId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" || parameters.TodoID == "" {
		self.sendError(frame.ID, 400, "conversationId and todoId are required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	var updated *models.Todo
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, parameters.TodoID, func(todo *models.Todo) error {
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
		self.sendError(frame.ID, 500, "reopening todo: "+err.Error())
		return
	}

	self.emitConversationTodoEvent(parameters.ConversationID, updated, "reopen")
	self.sendResponse(frame.ID, map[string]interface{}{"todo": updated})
}

// --- conversations.todos.update ---

func (self *webSocketConnection) handleConversationsTodosUpdate(frame requestFrame) {
	var parameters struct {
		ConversationID string   `json:"conversationId"`
		TodoID         string   `json:"todoId"`
		Title          string   `json:"title"`
		Priority       string   `json:"priority"`
		Tags           []string `json:"tags"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" || parameters.TodoID == "" {
		self.sendError(frame.ID, 400, "conversationId and todoId are required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	var updated *models.Todo
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ModifyTodo(ctx, parameters.TodoID, func(todo *models.Todo) error {
			if parameters.Title != "" {
				todo.Title = ptrto.Value(parameters.Title)
			}
			if parameters.Priority != "" {
				todo.Priority = ptrto.Value(models.TodoPriority(parameters.Priority))
			}
			if parameters.Tags != nil {
				todo.Tags = &parameters.Tags
			}
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updated = result
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "updating todo: "+err.Error())
		return
	}

	self.emitConversationTodoEvent(parameters.ConversationID, updated, "update")
	self.sendResponse(frame.ID, map[string]interface{}{"todo": updated})
}

// --- conversations.todos.delete ---

func (self *webSocketConnection) handleConversationsTodosDelete(frame requestFrame) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
		TodoID         string `json:"todoId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" || parameters.TodoID == "" {
		self.sendError(frame.ID, 400, "conversationId and todoId are required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		return tx.DeleteTodo(ctx, parameters.TodoID, nil)
	}); err != nil {
		self.sendError(frame.ID, 500, "deleting todo: "+err.Error())
		return
	}

	self.emitConversationTodoEvent(parameters.ConversationID, &models.Todo{ID: parameters.TodoID}, "delete")
	self.sendResponse(frame.ID, map[string]interface{}{"success": true})
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

func (self *webSocketConnection) emitConversationTodoEvent(conversationId string, todo *models.Todo, action string) {
	payload := map[string]interface{}{
		"conversationId": conversationId,
		"userId":         self.userId(),
		"action":         action,
		"todoId":         todo.ID,
	}
	if action != "delete" {
		payload["todo"] = todo
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeConversationTodos, payload)
}
