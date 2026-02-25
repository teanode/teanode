package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
)

// CompactResult holds the outcome of a conversation compaction.
type CompactResult struct {
	SummarizedMessages int `json:"summarizedMessages"`
	SummaryLength      int `json:"summaryLength"`
}

func conversationMessagesToChatMessages(messages []*models.ConversationMessage) []providers.ChatMessage {
	chatMessages := make([]providers.ChatMessage, 0, len(messages))
	for _, message := range messages {
		role := conversationMessageRole(*message)
		if role == "" {
			continue
		}
		chatMessage := providers.ChatMessage{
			Role:    role,
			Content: conversationMessageContentText(*message),
		}
		if role == "tool" {
			chatMessage.Name = message.GetToolName()
			chatMessage.ToolCallID = message.GetToolCallID()
		}
		chatMessages = append(chatMessages, chatMessage)
	}
	return chatMessages
}

// CompactConversation summarizes all messages in a conversation using the
// runner's buildMessages pipeline, reusing the cached system prompt and tool
// definitions. This enables prompt cache hits when the summarizer model matches
// the main model.
func (self *Runner) CompactConversation(ctx context.Context, conversationId string) (*CompactResult, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, fmt.Errorf("userId is required")
	}
	userId := user.ID
	if self.ResolveUser == nil {
		return nil, fmt.Errorf("ResolveUser is required")
	}
	// Load conversation history.
	history, err := listConversationMessages(ctx, conversationId)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("conversation is empty")
	}

	qualifiedModel, _ := self.resolveAgentModelAndName(ctx, self.Config.Models.GetDefault())
	if headerMessages, headerErr := listConversationMessages(ctx, conversationId); headerErr == nil {
		for index := len(headerMessages) - 1; index >= 0; index-- {
			modelName := headerMessages[index].GetModel()
			if modelName != "" {
				qualifiedModel = modelName
				break
			}
		}
	}
	// Build messages via the same pipeline used for normal runs.
	llmMessages := self.buildMessages(ctx, history, "", SystemPromptModeFull, self.SkillPrompts)
	// Resolve summarizer model.
	qualifiedModel = self.Config.Models.GetDefault()
	if summarizerModel := self.Config.Models.GetSummarizerModel(); summarizerModel != "" {
		qualifiedModel = summarizerModel
	}

	provider, bareModel, err := self.Providers.Resolve(qualifiedModel)
	if err != nil {
		return nil, fmt.Errorf("resolving summary model %q: %w", qualifiedModel, err)
	}

	contextWindow := self.Config.Models.GetContextWindow()
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	summaryText := formatStructuredSummary(
		summarizeMessagesWithFallback(
			ctx,
			provider,
			bareModel,
			llmMessages,
			contextWindow,
			prompts.StructuredSummaryDefaultFocus,
		),
	)

	// Persist summary to conversation.
	summaryMessage := newSummaryMessage(summaryText, time.Now().UnixMilli())
	if err := appendConversationMessage(ctx, userId, self.AgentID, conversationId, summaryMessage); err != nil {
		return nil, fmt.Errorf("saving summary: %w", err)
	}

	log.Debugf("conversation compacted (cache-aware): %d messages summarized", len(history))

	return &CompactResult{
		SummarizedMessages: len(history),
		SummaryLength:      len(summaryText),
	}, nil
}
