package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/provider"
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
	providers *provider.Registry,
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
	var builder strings.Builder
	for _, message := range messages {
		role := message.Role
		if role == "tool" && message.ToolName != "" {
			role = fmt.Sprintf("tool(%s)", message.ToolName)
		}
		content := message.ContentText()
		if len(content) > 2000 {
			content = content[:2000] + "..."
		}
		fmt.Fprintf(&builder, "[%s]: %s\n", role, content)
	}

	// Resolve summarizer model.
	qualifiedModel := configuration.Models.Default
	if configuration.Models.SummarizerModel != "" {
		qualifiedModel = configuration.Models.SummarizerModel
	}

	client, bareModel, err := providers.Resolve(qualifiedModel)
	if err != nil {
		return nil, fmt.Errorf("resolving summary model %q: %w", qualifiedModel, err)
	}

	summaryRequest := provider.ChatRequest{
		Model: bareModel,
		Messages: []provider.ChatMessage{
			{
				Role:    "system",
				Content: "Summarize the following conversation into a concise summary (max 500 words). Preserve key facts, decisions, tool results, and user preferences. Focus on information needed to continue the conversation naturally.",
			},
			{
				Role:    "user",
				Content: builder.String(),
			},
		},
	}

	var summaryText string
	response, err := client.ChatCompletion(ctx, summaryRequest)
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
