package agents

import (
	"context"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func ListConversationsForAgent(ctx context.Context, userId, agentId string) ([]*models.Conversation, error) {
	result := make([]*models.Conversation, 0)
	err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		items, err := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  ptrto.Value(userId),
			AgentID: ptrto.Value(agentId),
		}, nil)
		if err != nil {
			return err
		}
		result = append(result, items...)
		return nil
	})
	return result, err
}

func newSummaryMessage(summary string, timestamp int64) models.ConversationMessage {
	message := newTextMessage("system", summary, timestamp)
	stopReason := models.StopReason("context_summary")
	message.StopReason = &stopReason
	return message
}

func deleteConversationRecord(ctx context.Context, conversationId string) error {
	return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		err := transaction.DeleteConversation(ctx, conversationId, nil)
		if err == store.ErrNotFound {
			return nil
		}
		return err
	})
}
