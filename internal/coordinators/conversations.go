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
	return self.defaults.EnsureDefaultConversation(userId, agentId)
}

// SetDefaultConversation sets the default conversation for a user+agent and broadcasts the change.
func (self *Coordinator) SetDefaultConversation(userId, agentId, conversationId string) {
	if userId == "" {
		log.Warningf("set default conversation requires non-empty userId")
		return
	}
	self.defaults.SetDefaultConversation(userId, agentId, conversationId)
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
	changed := self.defaults.SetDefaultConversationIfUnset(userId, agentId, conversationId)
	if changed {
		self.pubsub.Broadcast(pubsub.EventTypeDefaultConversation, map[string]interface{}{
			"agentId":               agentId,
			"defaultConversationId": conversationId,
			"userId":                userId,
		})
	}
	return changed
}

// NewDefaultConversation creates a new conversation and sets it as the default.
func (self *Coordinator) NewDefaultConversation(userId, agentId, model string) string {
	if userId == "" {
		log.Warningf("new conversation requires non-empty userId")
		return ""
	}
	conversationId := self.defaults.NewDefaultConversation(userId, agentId)
	self.createConversation(userId, agentId, conversationId, model)

	self.pubsub.Broadcast(pubsub.EventTypeDefaultConversation, map[string]interface{}{
		"agentId":               agentId,
		"defaultConversationId": conversationId,
		"userId":                userId,
	})
	return conversationId
}

// createConversation creates a conversation in the store with the resolved model.
func (self *Coordinator) createConversation(userId, agentId, conversationId, model string) {
	if userId == "" {
		return
	}
	qualifiedModel := model
	if qualifiedModel == "" {
		if self.config != nil && self.config.Models != nil {
			qualifiedModel = self.config.Models.GetDefault()
		}
		_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			agent, err := transaction.GetAgent(ctx, agentId, nil)
			if err != nil || agent == nil {
				return nil
			}
			agentModel := agent.GetModel()
			if agentModel != "" {
				qualifiedModel = agentModel
			}
			return nil
		})
	}
	if qualifiedModel == "" {
		return
	}
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

// DeleteConversation deletes a conversation if it's not actively running.
func (self *Coordinator) DeleteConversation(userId, agentId, conversationId string) error {
	// Check not active.
	if self.GetActiveConversationRunner(conversationId) {
		return errConversationHasActiveRun
	}

	// Verify agent exists in store.
	var agentExists bool
	_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		agent, err := transaction.GetAgent(ctx, agentId, nil)
		if err == nil && agent != nil {
			agentExists = true
		}
		return nil
	})
	if !agentExists {
		return errAgentNotFound
	}
	deleteError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteConversation(ctx, conversationId, nil)
	})
	if deleteError != nil && deleteError != store.ErrNotFound {
		return deleteError
	}

	self.pubsub.Broadcast(pubsub.EventTypeConversations, nil)
	return nil
}

// ResolveOrCreateConversation resolves a conversation ID. If empty, creates a new one
// and sets it as default if unset. Returns the resolved conversation ID.
func (self *Coordinator) ResolveOrCreateConversation(userId, agentId, conversationId, model string) string {
	if conversationId == "" {
		conversationId = security.NewULID()
		self.createConversation(userId, agentId, conversationId, model)
		self.SetDefaultConversationIfUnset(userId, agentId, conversationId)
	} else {
		self.SetDefaultConversationIfUnset(userId, agentId, conversationId)
	}
	return conversationId
}
