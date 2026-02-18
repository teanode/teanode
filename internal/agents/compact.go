package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/providers"
)

// CompactResult holds the outcome of a conversation compaction.
type CompactResult struct {
	SummarizedMessages int `json:"summarizedMessages"`
	SummaryLength      int `json:"summaryLength"`
}

// CompactConversation summarizes all messages in a conversation and appends
// a summary message. Future runs will start from the summary, discarding
// everything before it.
func CompactConversation(
	ctx context.Context,
	store *conversations.Store,
	providerRegistry *providers.Registry,
	configuration *configs.Config,
	conversationId string,
) (*CompactResult, error) {
	messages, err := store.Load(conversationId)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("conversation is empty")
	}

	// Build text from all messages.
	conversationText := messagesText(messages, 0, 2000)

	// Resolve summarizer model.
	qualifiedModel := configuration.Models.Default
	if configuration.Models.SummarizerModel != "" {
		qualifiedModel = configuration.Models.SummarizerModel
	}

	provider, bareModel, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		return nil, fmt.Errorf("resolving summary model %q: %w", qualifiedModel, err)
	}

	summaryRequest := providers.ChatRequest{
		Model: bareModel,
		Messages: []providers.ChatMessage{
			{
				Role:    "system",
				Content: "Summarize the following conversation into a concise summary (max 500 words). Preserve key facts, decisions, tool results, and user preferences. Focus on information needed to continue the conversation naturally.",
			},
			{
				Role:    "user",
				Content: conversationText,
			},
		},
	}

	var summaryText string
	response, err := provider.ChatCompletion(ctx, summaryRequest)
	if err != nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		summaryText = fmt.Sprintf("[Earlier conversation with %d messages was dropped due to compaction]", len(messages))
	} else {
		summaryText = strings.TrimSpace(response.Choices[0].Message.Content)
	}

	// Persist summary to conversation.
	summaryMessage := conversations.NewSummaryMessage(summaryText, time.Now().UnixMilli())
	if err := store.Append(conversationId, summaryMessage); err != nil {
		return nil, fmt.Errorf("saving summary: %w", err)
	}

	log.Debugf("conversation compacted: %d messages summarized", len(messages))

	return &CompactResult{
		SummarizedMessages: len(messages),
		SummaryLength:      len(summaryText),
	}, nil
}
