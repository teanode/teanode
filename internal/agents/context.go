package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/providers"
)

const defaultContextWindow = 128000

// estimateTokens returns a rough token count using a character heuristic.
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// estimateMessageTokens estimates the token count for a single ChatMessage.
func estimateMessageTokens(message providers.ChatMessage) int {
	tokens := estimateTokens(message.ContentText()) + 4 // role + overhead
	for _, toolCall := range message.ToolCalls {
		tokens += estimateTokens(toolCall.Function.Name) + estimateTokens(toolCall.Function.Arguments) + 4
	}
	if message.Name != "" {
		tokens += estimateTokens(message.Name)
	}
	return tokens
}

// estimateToolDefsTokens estimates the token overhead of tool definitions.
func estimateToolDefsTokens(tools []providers.ToolDefinition) int {
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

// truncateOldToolResults caps old tool-result message content at maxChars.
// Messages in the last minKeep are preserved intact.
func truncateOldToolResults(messages []providers.ChatMessage, minKeep int, maxChars int) []providers.ChatMessage {
	if len(messages) <= minKeep {
		return messages
	}
	boundary := len(messages) - minKeep

	result := make([]providers.ChatMessage, len(messages))
	copy(result, messages)
	for index := 0; index < boundary; index++ {
		if text, ok := result[index].Content.(string); ok && result[index].Role == "tool" && len(text) > maxChars {
			result[index].Content = text[:maxChars] + "\n... (truncated)"
		}
	}
	return result
}

// findKeepBoundary walks backward from the target split point to find an index
// where we can safely split without breaking tool call/result pairs.
// Returns the index of the first message to keep.
func findKeepBoundary(messages []providers.ChatMessage, minKeep int) int {
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

// findLastSummaryIndex returns the index of the last context_summary message
// in history, or -1 if none exists.
func findLastSummaryIndex(messages []conversations.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "system" && messages[i].StopReason == "context_summary" {
			return i
		}
	}
	return -1
}

// messagesText builds a truncated text representation of conversation messages,
// collecting from the end to prioritize recent messages. Returns chronologically
// ordered text. Pass maxTotalChars <= 0 for no total limit.
func messagesText(messages []conversations.Message, maxTotalChars int, maxMessageChars int) string {
	var lines []string
	totalChars := 0

	for i := len(messages) - 1; i >= 0; i-- {
		role := messages[i].Role
		if role == "tool" && messages[i].ToolName != "" {
			role = fmt.Sprintf("tool(%s)", messages[i].ToolName)
		}

		content := messages[i].ContentText()
		if maxMessageChars > 0 && len(content) > maxMessageChars {
			content = content[:maxMessageChars] + "..."
		}

		line := fmt.Sprintf("[%s]: %s\n", role, content)
		if maxTotalChars > 0 && totalChars+len(line) > maxTotalChars {
			remaining := maxTotalChars - totalChars
			if remaining > 0 {
				lines = append(lines, line[:remaining])
			}
			break
		}
		lines = append(lines, line)
		totalChars += len(line)
	}

	// Reverse to chronological order.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	return strings.Join(lines, "")
}

// chatMessagesText builds a truncated text representation of provider chat
// messages, collecting from the end to prioritize recent messages. Returns
// chronologically ordered text. Pass maxTotalChars <= 0 for no total limit.
func chatMessagesText(messages []providers.ChatMessage, maxTotalChars int, maxMessageChars int) string {
	var lines []string
	totalChars := 0

	for i := len(messages) - 1; i >= 0; i-- {
		role := messages[i].Role
		if role == "tool" && messages[i].Name != "" {
			role = fmt.Sprintf("tool(%s)", messages[i].Name)
		}

		content := messages[i].ContentText()
		if maxMessageChars > 0 && len(content) > maxMessageChars {
			content = content[:maxMessageChars] + "..."
		}

		line := fmt.Sprintf("[%s]: %s\n", role, content)
		if maxTotalChars > 0 && totalChars+len(line) > maxTotalChars {
			remaining := maxTotalChars - totalChars
			if remaining > 0 {
				lines = append(lines, line[:remaining])
			}
			break
		}
		lines = append(lines, line)
		totalChars += len(line)
	}

	// Reverse to chronological order.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	return strings.Join(lines, "")
}

// compressContext checks whether the estimated token count exceeds the
// compression threshold and, if so, summarizes older messages via an LLM call.
func (self *Runner) compressContext(
	ctx context.Context,
	providerRegistry *providers.Registry,
	config *configs.Config,
	messages []providers.ChatMessage,
	toolDefs []providers.ToolDefinition,
	conversationId string,
	contextWindow int,
	limits configs.AgentLimits,
) ([]providers.ChatMessage, error) {
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}

	// Estimate total tokens.
	total := estimateToolDefsTokens(toolDefs)
	for _, message := range messages {
		total += estimateMessageTokens(message)
	}

	threshold := int(float64(contextWindow) * limits.CompressionThreshold)
	if total <= threshold {
		return messages, nil
	}

	log.Debugf("context compression triggered: estimated %d tokens, threshold %d", total, threshold)

	// Find the split point: keep system prompt (index 0) + recent messages.
	// messages[0] is always the system prompt.
	keepIdx := findKeepBoundary(messages[1:], limits.MinKeepMessages) + 1 // +1 for system prompt offset
	if keepIdx <= 1 {
		// Nothing to compress.
		return messages, nil
	}

	// Messages to summarize: messages[1:keepIdx] (skip system prompt at 0).
	toSummarize := messages[1:keepIdx]

	// Build summary text from messages.
	summaryInput := chatMessagesText(toSummarize, 0, 2000)

	// Pick summary model and resolve its provider.
	summaryQualifiedModel := config.Models.Default
	if config.Models.SummarizerModel != "" {
		summaryQualifiedModel = config.Models.SummarizerModel
	}

	summaryClient, summaryBareModel, resolveErr := providerRegistry.Resolve(summaryQualifiedModel)
	if resolveErr != nil {
		return messages, fmt.Errorf("resolving summary model %q: %w", summaryQualifiedModel, resolveErr)
	}

	summaryRequest := providers.ChatRequest{
		Model: summaryBareModel,
		Messages: []providers.ChatMessage{
			{
				Role:    "system",
				Content: "Summarize the following conversation into a concise summary (max 500 words). Preserve key facts, decisions, tool results, and user preferences. Focus on information needed to continue the conversation naturally.",
			},
			{
				Role:    "user",
				Content: summaryInput,
			},
		},
	}

	var summaryText string
	response, err := summaryClient.ChatCompletion(ctx, summaryRequest)
	if err != nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.ContentText()) == "" {
		// Fallback: drop old messages without summary.
		log.Debugf("context summarization failed, falling back to drop: %v", err)
		summaryText = fmt.Sprintf("[Earlier conversation with %d messages was dropped due to context limits]", len(toSummarize))
	} else {
		summaryText = strings.TrimSpace(response.Choices[0].Message.ContentText())
	}

	// Persist summary to conversation.
	summaryMessage := conversations.NewSummaryMessage(summaryText, time.Now().UnixMilli())
	if err := self.Conversations.Append(conversationId, summaryMessage); err != nil {
		log.Debugf("failed to persist context summary: %v", err)
	}

	// Build compressed messages: system prompt + summary + kept messages.
	compressed := make([]providers.ChatMessage, 0, 2+len(messages)-keepIdx)
	compressed = append(compressed, messages[0]) // system prompt
	compressed = append(compressed, providers.ChatMessage{
		Role:    "system",
		Content: "Previous conversation summary:\n" + summaryText,
	})
	compressed = append(compressed, messages[keepIdx:]...)

	log.Debugf("context compressed: %d messages -> %d messages (dropped %d, kept %d)",
		len(messages), len(compressed), len(toSummarize), len(messages)-keepIdx)

	return compressed, nil
}
