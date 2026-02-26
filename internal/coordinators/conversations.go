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
// All conversation management methods intentionally use the coordinator's own context (self.ctx)
// rather than a caller-provided context because these operations manage global state that must
// persist regardless of individual request lifecycles (e.g. a disconnecting websocket should
// not cancel a default-conversation write).
func (self *Coordinator) EnsureDefaultConversation(userId, agentId string) string {
	var conversationId string
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		defaultConversation, err := transaction.FindDefaultConversation(ctx, userId, agentId, nil)
		if err != nil {
			return err
		}
		if defaultConversation != nil {
			conversationId = defaultConversation.ID
			return nil
		}
		// Fall back to the most recent conversation for this agent.
		conversations, err := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if err != nil {
			return err
		}
		if len(conversations) > 0 {
			conversationId = conversations[0].ID
			return nil
		}
		return nil
	}); err != nil {
		log.Warningf("failed to find default conversation for user %q agent %q from store: %v", userId, agentId, err)
	}
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
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		defaultConversation, err := transaction.FindDefaultConversation(ctx, userId, agentId, nil)
		if err != nil {
			return err
		}
		alreadySet = defaultConversation != nil
		return nil
	}); err != nil {
		log.Warningf("failed to set default conversation for user %q agent %q conversation %q: %v", userId, agentId, conversationId, err)
	}
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
		_, err := transaction.CreateConversation(ctx, &models.Conversation{
			ID:      conversationId,
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		return err
	}); err != nil {
		log.Errorf("creating conversation file: %v", err)
	}
}

// persistDefaultConversation ensures the conversation exists and sets it as the default.
func (self *Coordinator) persistDefaultConversation(userId, agentId, conversationId string) {
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		// Ensure the conversation exists in the store.
		if _, err := transaction.GetConversation(ctx, conversationId, nil); err != nil {
			if _, err := transaction.CreateConversation(ctx, &models.Conversation{
				ID:      conversationId,
				UserID:  ptrto.Value(userId),
				AgentID: ptrto.Value(agentId),
			}, nil); err != nil {
				return err
			}
		}
		return transaction.SetDefaultConversation(ctx, conversationId, nil)
	}); err != nil {
		log.Warningf("persisting default conversation failed for user %q agent %q conversation %q: %v", userId, agentId, conversationId, err)
	}
}
