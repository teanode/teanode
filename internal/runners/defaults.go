package runners

import (
	"context"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

// DefaultConversationManager manages per-user default conversation state,
// backed by the store.
type DefaultConversationManager struct {
	ctx context.Context
}

// NewDefaultConversationManager creates a new manager.
func NewDefaultConversationManager(ctx context.Context) *DefaultConversationManager {
	return &DefaultConversationManager{ctx: ctx}
}

func (self *DefaultConversationManager) EnsureDefaultConversation(userId, agentId string) string {
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

func (self *DefaultConversationManager) SetDefaultConversation(userId, agentId, conversationId string) {
	self.persistDefaultConversation(userId, agentId, conversationId)
}

func (self *DefaultConversationManager) SetDefaultConversationIfUnset(userId, agentId, conversationId string) bool {
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
	return true
}

func (self *DefaultConversationManager) NewDefaultConversation(userId, agentId string) string {
	conversationId := security.NewULID()
	self.persistDefaultConversation(userId, agentId, conversationId)
	return conversationId
}

func (self *DefaultConversationManager) persistDefaultConversation(userId string, agentId string, conversationId string) {
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
