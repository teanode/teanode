package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/session"
)

const (
	defaultContextWindow = 128000
	compressionThreshold = 0.80
	minKeepMessages      = 10
	maxToolResultChars   = 8000
)

// estimateTokens returns a rough token count using a character heuristic.
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// estimateMessageTokens estimates the token count for a single ChatMessage.
func estimateMessageTokens(message provider.ChatMessage) int {
	tokens := estimateTokens(message.Content) + 4 // role + overhead
	for _, toolCall := range message.ToolCalls {
		tokens += estimateTokens(toolCall.Function.Name) + estimateTokens(toolCall.Function.Arguments) + 4
	}
	if message.Name != "" {
		tokens += estimateTokens(message.Name)
	}
	return tokens
}

// estimateToolDefsTokens estimates the token overhead of tool definitions.
func estimateToolDefsTokens(tools []provider.ToolDef) int {
	tokens := 0
	for _, tool := range tools {
		tokens += estimateTokens(tool.Function.Name) + estimateTokens(tool.Function.Description)
		if tool.Function.Parameters != nil {
			data, _ := json.Marshal(tool.Function.Parameters)
			tokens += estimateTokens(string(data))
		}
	}
	return tokens
}

// truncateOldToolResults caps old tool-result message content at maxToolResultChars.
// Messages in the last minKeepMessages are preserved intact.
func truncateOldToolResults(messages []provider.ChatMessage) []provider.ChatMessage {
	if len(messages) <= minKeepMessages {
		return messages
	}
	boundary := len(messages) - minKeepMessages

	result := make([]provider.ChatMessage, len(messages))
	copy(result, messages)
	for index := 0; index < boundary; index++ {
		if result[index].Role == "tool" && len(result[index].Content) > maxToolResultChars {
			result[index].Content = result[index].Content[:maxToolResultChars] + "\n... (truncated)"
		}
	}
	return result
}

// findKeepBoundary walks backward from the target split point to find an index
// where we can safely split without breaking tool call/result pairs.
// Returns the index of the first message to keep.
func findKeepBoundary(messages []provider.ChatMessage, minKeep int) int {
	if len(messages) <= minKeep {
		return 0
	}
	target := len(messages) - minKeep

	// Walk backward from target to find a safe split point.
	index := target
	for index > 0 {
		// If the message at index is a tool result, we need to include its
		// parent assistant message, so move backward past all tool results
		// and include the assistant message that triggered them.
		if messages[index].Role == "tool" {
			for index > 0 && messages[index].Role == "tool" {
				index--
			}
			// index now points at the assistant message with tool calls; include it
			continue
		}

		// If the message just before index is an assistant with tool calls,
		// and index is a tool result for it, we already handled that above.
		// But check if index-1 is an assistant with tool calls whose results
		// would be split off.
		if index > 0 && messages[index-1].Role == "assistant" && len(messages[index-1].ToolCalls) > 0 {
			// The assistant's tool results follow it; keep the assistant with its results.
			index--
			continue
		}

		break
	}

	return index
}

// compressContext checks whether the estimated token count exceeds the
// compression threshold and, if so, summarizes older messages via an LLM call.
func (self *Runner) compressContext(
	ctx context.Context,
	providers *provider.Registry,
	cfg *config.Config,
	messages []provider.ChatMessage,
	toolDefs []provider.ToolDef,
	sessionKey string,
	contextWindow int,
) ([]provider.ChatMessage, error) {
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}

	// Estimate total tokens.
	total := estimateToolDefsTokens(toolDefs)
	for _, message := range messages {
		total += estimateMessageTokens(message)
	}

	threshold := int(float64(contextWindow) * compressionThreshold)
	if total <= threshold {
		return messages, nil
	}

	log.Debugf("context compression triggered: estimated %d tokens, threshold %d", total, threshold)

	// Find the split point: keep system prompt (index 0) + recent messages.
	// messages[0] is always the system prompt.
	keepIdx := findKeepBoundary(messages[1:], minKeepMessages) + 1 // +1 for system prompt offset
	if keepIdx <= 1 {
		// Nothing to compress.
		return messages, nil
	}

	// Messages to summarize: messages[1:keepIdx] (skip system prompt at 0).
	toSummarize := messages[1:keepIdx]

	// Build summary text from messages.
	var builder strings.Builder
	for _, message := range toSummarize {
		role := message.Role
		if role == "tool" && message.Name != "" {
			role = fmt.Sprintf("tool(%s)", message.Name)
		}
		content := message.Content
		if len(content) > 2000 {
			content = content[:2000] + "..."
		}
		fmt.Fprintf(&builder, "[%s]: %s\n", role, content)
	}

	// Pick summary model and resolve its provider.
	summaryQualifiedModel := cfg.Models.Default
	if cfg.Models.SummaryModel != "" {
		summaryQualifiedModel = cfg.Models.SummaryModel
	} else if cfg.Models.TitleModel != "" {
		summaryQualifiedModel = cfg.Models.TitleModel
	}

	summaryClient, summaryBareModel, resolveErr := providers.Resolve(summaryQualifiedModel)
	if resolveErr != nil {
		return messages, fmt.Errorf("resolving summary model %q: %w", summaryQualifiedModel, resolveErr)
	}

	summaryRequest := provider.ChatRequest{
		Model: summaryBareModel,
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
	response, err := summaryClient.ChatCompletion(ctx, summaryRequest)
	if err != nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		// Fallback: drop old messages without summary.
		log.Debugf("context summarization failed, falling back to drop: %v", err)
		summaryText = fmt.Sprintf("[Earlier conversation with %d messages was dropped due to context limits]", len(toSummarize))
	} else {
		summaryText = strings.TrimSpace(response.Choices[0].Message.Content)
	}

	// Persist summary to session.
	summaryMessage := session.NewSummaryMessage(summaryText, time.Now().UnixMilli())
	if err := self.Sessions.Append(sessionKey, summaryMessage); err != nil {
		log.Debugf("failed to persist context summary: %v", err)
	}

	// Build compressed messages: system prompt + summary + kept messages.
	compressed := make([]provider.ChatMessage, 0, 2+len(messages)-keepIdx)
	compressed = append(compressed, messages[0]) // system prompt
	compressed = append(compressed, provider.ChatMessage{
		Role:    "system",
		Content: "Previous conversation summary:\n" + summaryText,
	})
	compressed = append(compressed, messages[keepIdx:]...)

	log.Debugf("context compressed: %d messages -> %d messages (dropped %d, kept %d)",
		len(messages), len(compressed), len(toSummarize), len(messages)-keepIdx)

	return compressed, nil
}
