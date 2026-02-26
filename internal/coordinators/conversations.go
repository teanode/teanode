package coordinators

import (
	"context"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

// EnsureDefaultConversation returns the default conversation for a user+agent, creating one if needed.
func (self *Coordinator) EnsureDefaultConversation(userId, agentId string) string {
	var conversationId string
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		defaultConversation, findError := transaction.FindDefaultConversation(ctx, userId, agentId, nil)
		if findError == nil && defaultConversation != nil {
			conversationId = defaultConversation.ID
			return nil
		}
		// Fall back to the most recent conversation for this agent.
		conversations, listError := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if listError == nil && len(conversations) > 0 {
			conversationId = conversations[0].ID
			return nil
		}
		return nil
	})
	if conversationId != "" {
		return conversationId
	}
	conversationId = security.NewULID()
	self.persistDefaultConversation(userId, agentId, conversationId)
	return conversationId
}

// SetDefaultConversation sets the default conversation for a user+agent and broadcasts the change.
func (self *Coordinator) SetDefaultConversation(userId, agentId, conversationId string) {
	if userId == "" {
		log.Warningf("set default conversation requires non-empty userId")
		return
	}
	self.persistDefaultConversation(userId, agentId, conversationId)
	self.pubsub.Broadcast(pubsub.EventTypeDefaultConversation, map[string]interface{}{
		"agentId":               agentId,
		"defaultConversationId": conversationId,
		"userId":                userId,
	})
}

// SetDefaultConversationIfUnset sets the default conversation only if none is currently set.
// Returns true if the default was changed.
func (self *Coordinator) SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool {
	if userId == "" {
		log.Warningf("set default conversation-if-unset requires non-empty userId")
		return false
	}
	var alreadySet bool
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		defaultConversation, findError := transaction.FindDefaultConversation(ctx, userId, agentId, nil)
		if findError == nil && defaultConversation != nil {
			alreadySet = true
		}
		return nil
	})
	if alreadySet {
		return false
	}
	self.persistDefaultConversation(userId, agentId, conversationId)
	self.pubsub.Broadcast(pubsub.EventTypeDefaultConversation, map[string]interface{}{
		"agentId":               agentId,
		"defaultConversationId": conversationId,
		"userId":                userId,
	})
	return true
}

// NewDefaultConversation creates a new conversation and sets it as the default.
func (self *Coordinator) NewDefaultConversation(userId, agentId string) string {
	if userId == "" {
		log.Warningf("new conversation requires non-empty userId")
		return ""
	}
	conversationId := security.NewULID()
	self.persistDefaultConversation(userId, agentId, conversationId)
	self.createConversation(userId, agentId, conversationId)

	self.pubsub.Broadcast(pubsub.EventTypeDefaultConversation, map[string]interface{}{
		"agentId":               agentId,
		"defaultConversationId": conversationId,
		"userId":                userId,
	})
	return conversationId
}

// createConversation creates a conversation in the store with the resolved model.
func (self *Coordinator) createConversation(userId, agentId, conversationId string) {
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateConversation(ctx, &models.Conversation{
			ID:      conversationId,
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		return createError
	}); err != nil {
		log.Errorf("creating conversation file: %v", err)
	}
}

// persistDefaultConversation ensures the conversation exists and sets it as the default.
func (self *Coordinator) persistDefaultConversation(userId, agentId, conversationId string) {
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		// Ensure the conversation exists in the store.
		if _, err := transaction.GetConversation(ctx, conversationId, nil); err != nil {
			if _, createError := transaction.CreateConversation(ctx, &models.Conversation{
				ID:      conversationId,
				UserID:  ptrto.Value(userId),
				AgentID: ptrto.Value(agentId),
			}, nil); createError != nil {
				return createError
			}
		}
		return transaction.SetDefaultConversation(ctx, conversationId, nil)
	}); err != nil {
		log.Warningf("persisting default conversation failed userId=%s agentId=%s conversationId=%s error=%v", userId, agentId, conversationId, err)
	}
}
