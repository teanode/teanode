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

const defaultMinKeepMessages = 6

// CompactResult holds the outcome of a conversation compaction.
type CompactResult struct {
	SummarizedMessages int `json:"summarizedMessages"`
	KeptMessages       int `json:"keptMessages"`
	SummaryLength      int `json:"summaryLength"`
}

// CompactConversation summarizes older messages in a conversation and appends
// a summary message. minKeepMessages controls how many recent messages to
// preserve; 0 uses the default of 6.
func CompactConversation(
	ctx context.Context,
	store *conversations.Store,
	providers *provider.Registry,
	configuration *configs.Config,
	conversationId string,
	minKeepMessages int,
) (*CompactResult, error) {
	if minKeepMessages <= 0 {
		minKeepMessages = defaultMinKeepMessages
	}

	messages, err := store.Load(conversationId)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}
	if len(messages) <= minKeepMessages {
		return nil, fmt.Errorf("conversation has too few messages to compact (%d messages, need more than %d)", len(messages), minKeepMessages)
	}

	// Find a safe split point that doesn't break tool call/result pairs.
	// We work on conversations.Message, adapting the findKeepBoundary logic.
	keepIndex := findConversationKeepBoundary(messages, minKeepMessages)
	if keepIndex <= 0 {
		return nil, fmt.Errorf("nothing to compact")
	}

	toSummarize := messages[:keepIndex]

	// Build text from messages to summarize.
	var builder strings.Builder
	for _, message := range toSummarize {
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
		summaryText = fmt.Sprintf("[Earlier conversation with %d messages was dropped due to compaction]", len(toSummarize))
	} else {
		summaryText = strings.TrimSpace(response.Choices[0].Message.Content)
	}

	// Persist summary to conversation.
	summaryMessage := conversations.NewSummaryMessage(summaryText, time.Now().UnixMilli())
	if err := store.Append(conversationId, summaryMessage); err != nil {
		return nil, fmt.Errorf("saving summary: %w", err)
	}

	keptMessages := len(messages) - keepIndex
	log.Debugf("conversation compacted: %d messages summarized, %d kept", len(toSummarize), keptMessages)

	return &CompactResult{
		SummarizedMessages: len(toSummarize),
		KeptMessages:       keptMessages,
		SummaryLength:      len(summaryText),
	}, nil
}

// findConversationKeepBoundary walks backward from the target split point to
// find an index where we can safely split without breaking tool call/result pairs.
// Returns the index of the first message to keep (everything before it gets summarized).
func findConversationKeepBoundary(messages []conversations.Message, minKeep int) int {
	if len(messages) <= minKeep {
		return 0
	}
	target := len(messages) - minKeep

	index := target
	for index > 0 {
		if messages[index].Role == "tool" {
			for index > 0 && messages[index].Role == "tool" {
				index--
			}
			// index now points at the assistant message with tool calls; include it
			continue
		}

		if index > 0 && messages[index-1].Role == "assistant" && len(messages[index-1].ToolCalls) > 0 {
			index--
			continue
		}

		break
	}

	return index
}
