package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
)

const defaultContextWindow = 128000
const defaultSummaryChunkTokens = 12000
const defaultSummaryMaxMessageChars = 2000
const defaultSummaryOversizedMessageTokens = 8000
const defaultHardClearToolMultiplier = 4
const defaultHardClearedToolPlaceholder = "[Old tool result content cleared due to context limits]"

type criticalFacts struct {
	Decisions       []string `json:"decisions"`
	Todos           []string `json:"todos"`
	Constraints     []string `json:"constraints"`
	UserPreferences []string `json:"userPreferences"`
	OpenQuestions   []string `json:"openQuestions"`
}

type structuredSummary struct {
	Summary       string        `json:"summary"`
	CriticalFacts criticalFacts `json:"criticalFacts"`
}

// estimateTokens returns a rough token count using a character heuristic.
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

func stripDetailsFields(value interface{}) interface{} {
	switch typedValue := value.(type) {
	case map[string]interface{}:
		sanitized := make(map[string]interface{}, len(typedValue))
		for key, item := range typedValue {
			if strings.EqualFold(key, "details") {
				continue
			}
			sanitized[key] = stripDetailsFields(item)
		}
		return sanitized
	case []interface{}:
		sanitized := make([]interface{}, len(typedValue))
		for index, item := range typedValue {
			sanitized[index] = stripDetailsFields(item)
		}
		return sanitized
	default:
		return value
	}
}

func sanitizeToolResultForCompaction(text string) string {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" || !json.Valid([]byte(trimmedText)) {
		return text
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmedText), &parsed); err != nil {
		return text
	}
	sanitized := stripDetailsFields(parsed)
	serialized, err := json.Marshal(sanitized)
	if err != nil {
		return text
	}
	return string(serialized)
}

// estimateMessageTokens estimates the token count for a single ChatMessage.
func estimateMessageTokens(message providers.ChatMessage) int {
	contentText := message.ContentText()
	if message.Role == "tool" {
		contentText = sanitizeToolResultForCompaction(contentText)
	}
	tokens := estimateTokens(contentText) + 4 // role + overhead
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

func trimToolResultText(text string, maxChars int) string {
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	headChars := int(float64(maxChars) * 0.75)
	if headChars <= 0 {
		headChars = maxChars / 2
	}
	if headChars >= maxChars {
		headChars = maxChars / 2
	}
	tailChars := maxChars - headChars
	if tailChars < 0 {
		tailChars = 0
	}
	head := text[:headChars]
	tail := ""
	if tailChars > 0 {
		tail = text[len(text)-tailChars:]
	}
	return fmt.Sprintf("%s\n...\n%s\n... (truncated)", head, tail)
}

// truncateOldToolResults applies a two-tier pruning strategy for old tool results:
// soft-trim large results and hard-clear very large results. Messages in the last
// minKeep are preserved intact.
func truncateOldToolResults(messages []providers.ChatMessage, minKeep int, maxChars int) []providers.ChatMessage {
	if len(messages) <= minKeep {
		return messages
	}
	boundary := len(messages) - minKeep

	result := make([]providers.ChatMessage, len(messages))
	copy(result, messages)
	hardLimitChars := maxChars * defaultHardClearToolMultiplier
	for index := 0; index < boundary; index++ {
		text, ok := result[index].Content.(string)
		if !ok || result[index].Role != "tool" {
			continue
		}
		if maxChars > 0 && len(text) > hardLimitChars {
			result[index].Content = defaultHardClearedToolPlaceholder
			continue
		}
		if maxChars > 0 && len(text) > maxChars {
			result[index].Content = trimToolResultText(text, maxChars)
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

// expandKeepBoundaryForRecentTokens moves the keep boundary earlier (smaller index)
// until at least minKeepRecentTokens are preserved in the tail. Expects messages to
// include the system prompt at index 0; keepIdx is an absolute index into messages.
func expandKeepBoundaryForRecentTokens(messages []providers.ChatMessage, keepIdx int, minKeepRecentTokens int) int {
	if minKeepRecentTokens <= 0 {
		return keepIdx
	}
	if keepIdx < 1 || keepIdx >= len(messages) {
		return keepIdx
	}
	keptTokens := 0
	for index := keepIdx; index < len(messages); index++ {
		keptTokens += estimateMessageTokens(messages[index])
	}
	for keepIdx > 1 && keptTokens < minKeepRecentTokens {
		keepIdx--
		keptTokens += estimateMessageTokens(messages[keepIdx])
	}
	return keepIdx
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
		if messages[i].Role == "tool" {
			content = sanitizeToolResultForCompaction(content)
		}
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

func normalizeFactLines(lines []string) []string {
	seen := make(map[string]struct{}, len(lines))
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			continue
		}
		normalizedKey := strings.ToLower(trimmedLine)
		if _, exists := seen[normalizedKey]; exists {
			continue
		}
		seen[normalizedKey] = struct{}{}
		normalized = append(normalized, trimmedLine)
	}
	if len(normalized) > 8 {
		return normalized[:8]
	}
	return normalized
}

func normalizeStructuredSummary(summary structuredSummary) structuredSummary {
	summary.Summary = strings.TrimSpace(summary.Summary)
	summary.CriticalFacts.Decisions = normalizeFactLines(summary.CriticalFacts.Decisions)
	summary.CriticalFacts.Todos = normalizeFactLines(summary.CriticalFacts.Todos)
	summary.CriticalFacts.Constraints = normalizeFactLines(summary.CriticalFacts.Constraints)
	summary.CriticalFacts.UserPreferences = normalizeFactLines(summary.CriticalFacts.UserPreferences)
	summary.CriticalFacts.OpenQuestions = normalizeFactLines(summary.CriticalFacts.OpenQuestions)
	return summary
}

func appendFactSection(builder *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	builder.WriteString(title)
	builder.WriteString(":\n")
	for _, line := range lines {
		builder.WriteString("- ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

func formatStructuredSummary(summary structuredSummary) string {
	normalizedSummary := normalizeStructuredSummary(summary)
	if normalizedSummary.Summary == "" {
		normalizedSummary.Summary = "Context summary unavailable."
	}
	var builder strings.Builder
	builder.WriteString(normalizedSummary.Summary)

	hasCriticalFacts := len(normalizedSummary.CriticalFacts.Decisions) > 0 ||
		len(normalizedSummary.CriticalFacts.Todos) > 0 ||
		len(normalizedSummary.CriticalFacts.Constraints) > 0 ||
		len(normalizedSummary.CriticalFacts.UserPreferences) > 0 ||
		len(normalizedSummary.CriticalFacts.OpenQuestions) > 0
	if !hasCriticalFacts {
		return builder.String()
	}

	builder.WriteString("\n\nCritical facts:\n")
	appendFactSection(&builder, "Decisions", normalizedSummary.CriticalFacts.Decisions)
	appendFactSection(&builder, "Todos", normalizedSummary.CriticalFacts.Todos)
	appendFactSection(&builder, "Constraints", normalizedSummary.CriticalFacts.Constraints)
	appendFactSection(&builder, "User preferences", normalizedSummary.CriticalFacts.UserPreferences)
	appendFactSection(&builder, "Open questions", normalizedSummary.CriticalFacts.OpenQuestions)
	return strings.TrimSpace(builder.String())
}

func mergeStructuredSummaries(baseSummary, incomingSummary structuredSummary) structuredSummary {
	mergedSummary := structuredSummary{
		Summary: incomingSummary.Summary,
		CriticalFacts: criticalFacts{
			Decisions:       append(append([]string{}, baseSummary.CriticalFacts.Decisions...), incomingSummary.CriticalFacts.Decisions...),
			Todos:           append(append([]string{}, baseSummary.CriticalFacts.Todos...), incomingSummary.CriticalFacts.Todos...),
			Constraints:     append(append([]string{}, baseSummary.CriticalFacts.Constraints...), incomingSummary.CriticalFacts.Constraints...),
			UserPreferences: append(append([]string{}, baseSummary.CriticalFacts.UserPreferences...), incomingSummary.CriticalFacts.UserPreferences...),
			OpenQuestions:   append(append([]string{}, baseSummary.CriticalFacts.OpenQuestions...), incomingSummary.CriticalFacts.OpenQuestions...),
		},
	}
	if strings.TrimSpace(mergedSummary.Summary) == "" {
		mergedSummary.Summary = baseSummary.Summary
	}
	return normalizeStructuredSummary(mergedSummary)
}

func parseStructuredSummaryResponse(rawText string) structuredSummary {
	trimmedText := strings.TrimSpace(rawText)
	parsedSummary := structuredSummary{}
	if trimmedText == "" {
		return parsedSummary
	}

	if strings.HasPrefix(trimmedText, "```") {
		trimmedText = strings.TrimPrefix(trimmedText, "```json")
		trimmedText = strings.TrimPrefix(trimmedText, "```")
		trimmedText = strings.TrimSuffix(trimmedText, "```")
		trimmedText = strings.TrimSpace(trimmedText)
	}

	if json.Unmarshal([]byte(trimmedText), &parsedSummary) == nil {
		return normalizeStructuredSummary(parsedSummary)
	}
	if startIndex := strings.Index(trimmedText, "{"); startIndex >= 0 {
		if endIndex := strings.LastIndex(trimmedText, "}"); endIndex > startIndex {
			candidate := strings.TrimSpace(trimmedText[startIndex : endIndex+1])
			if json.Unmarshal([]byte(candidate), &parsedSummary) == nil {
				return normalizeStructuredSummary(parsedSummary)
			}
		}
	}

	return structuredSummary{Summary: strings.TrimSpace(rawText)}
}

func summarizeChunk(
	ctx context.Context,
	provider providers.Provider,
	model string,
	chunkText string,
	previousSummary string,
	focus string,
) (structuredSummary, error) {
	if strings.TrimSpace(chunkText) == "" {
		return structuredSummary{}, nil
	}

	userPrompt := prompts.BuildStructuredSummaryUserPrompt(previousSummary, focus, chunkText)

	summaryRequest := providers.ChatRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{
				Role:    "system",
				Content: prompts.StructuredSummarySystemPrompt,
			},
			{
				Role:    "user",
				Content: userPrompt,
			},
		},
	}
	response, err := provider.ChatCompletion(ctx, summaryRequest)
	if err != nil {
		return structuredSummary{}, err
	}
	if len(response.Choices) == 0 {
		return structuredSummary{}, fmt.Errorf("empty summary response")
	}
	content := strings.TrimSpace(response.Choices[0].Message.ContentText())
	if content == "" {
		return structuredSummary{}, fmt.Errorf("empty summary content")
	}
	return parseStructuredSummaryResponse(content), nil
}

func splitMessagesByTokenBudget(messages []providers.ChatMessage, maxTokens int) [][]providers.ChatMessage {
	if len(messages) == 0 {
		return nil
	}
	if maxTokens <= 0 {
		maxTokens = defaultSummaryChunkTokens
	}
	chunks := make([][]providers.ChatMessage, 0, 4)
	currentChunk := make([]providers.ChatMessage, 0, 16)
	currentTokens := 0
	for _, message := range messages {
		messageTokens := estimateMessageTokens(message)
		if len(currentChunk) > 0 && currentTokens+messageTokens > maxTokens {
			chunks = append(chunks, currentChunk)
			currentChunk = make([]providers.ChatMessage, 0, 16)
			currentTokens = 0
		}
		currentChunk = append(currentChunk, message)
		currentTokens += messageTokens
	}
	if len(currentChunk) > 0 {
		chunks = append(chunks, currentChunk)
	}
	return chunks
}

func summarizeMessagesInStages(
	ctx context.Context,
	provider providers.Provider,
	model string,
	messages []providers.ChatMessage,
	contextWindow int,
	focus string,
) (structuredSummary, error) {
	if len(messages) == 0 {
		return structuredSummary{Summary: prompts.NoPriorHistorySummary}, nil
	}

	chunkBudgetTokens := contextWindow / 3
	if chunkBudgetTokens < 2000 {
		chunkBudgetTokens = 2000
	}
	if chunkBudgetTokens > defaultSummaryChunkTokens {
		chunkBudgetTokens = defaultSummaryChunkTokens
	}

	chunks := splitMessagesByTokenBudget(messages, chunkBudgetTokens)
	if len(chunks) == 0 {
		return structuredSummary{Summary: prompts.NoPriorHistorySummary}, nil
	}

	mergedSummary := structuredSummary{}
	for _, chunk := range chunks {
		chunkText := chatMessagesText(chunk, 0, defaultSummaryMaxMessageChars)
		chunkSummary, err := summarizeChunk(ctx, provider, model, chunkText, formatStructuredSummary(mergedSummary), focus)
		if err != nil {
			return structuredSummary{}, err
		}
		mergedSummary = mergeStructuredSummaries(mergedSummary, chunkSummary)
	}
	return normalizeStructuredSummary(mergedSummary), nil
}

func buildLastMessagesFallback(messages []providers.ChatMessage) structuredSummary {
	if len(messages) == 0 {
		return structuredSummary{Summary: prompts.NoPriorHistorySummary}
	}
	tailCount := 8
	if len(messages) < tailCount {
		tailCount = len(messages)
	}
	tailMessages := messages[len(messages)-tailCount:]
	fallbackText := strings.TrimSpace(chatMessagesText(tailMessages, 4000, 500))
	if fallbackText == "" {
		fallbackText = "Recent context unavailable."
	}
	return structuredSummary{
		Summary: "Context summary degraded due to size limits. Preserved recent turns:\n" + fallbackText,
	}
}

func summarizeMessagesWithFallback(
	ctx context.Context,
	provider providers.Provider,
	model string,
	messages []providers.ChatMessage,
	contextWindow int,
	focus string,
) structuredSummary {
	fullSummary, err := summarizeMessagesInStages(ctx, provider, model, messages, contextWindow, focus)
	if err == nil && strings.TrimSpace(fullSummary.Summary) != "" {
		return fullSummary
	}
	log.Debugf("summary full pass failed, retrying without oversized messages: %v", err)

	oversizedThreshold := defaultSummaryOversizedMessageTokens
	if contextWindow > 0 && contextWindow/4 < oversizedThreshold {
		oversizedThreshold = contextWindow / 4
	}
	if oversizedThreshold < 2000 {
		oversizedThreshold = 2000
	}
	filteredMessages := make([]providers.ChatMessage, 0, len(messages))
	for _, message := range messages {
		if estimateMessageTokens(message) > oversizedThreshold {
			continue
		}
		filteredMessages = append(filteredMessages, message)
	}
	if len(filteredMessages) > 0 {
		partialSummary, partialErr := summarizeMessagesInStages(ctx, provider, model, filteredMessages, contextWindow, focus)
		if partialErr == nil && strings.TrimSpace(partialSummary.Summary) != "" {
			return partialSummary
		}
		log.Debugf("summary partial pass failed, falling back to deterministic summary: %v", partialErr)
	}

	return buildLastMessagesFallback(messages)
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
	keepIdx = expandKeepBoundaryForRecentTokens(messages, keepIdx, limits.MinKeepRecentTokens)
	if keepIdx <= 1 {
		// Nothing to compress.
		return messages, nil
	}

	// Messages to summarize: messages[1:keepIdx] (skip system prompt at 0).
	toSummarize := messages[1:keepIdx]

	// Pick summary model and resolve its provider.
	summaryQualifiedModel := config.Models.Default
	if config.Models.SummarizerModel != "" {
		summaryQualifiedModel = config.Models.SummarizerModel
	}

	summaryClient, summaryBareModel, resolveErr := providerRegistry.Resolve(summaryQualifiedModel)
	if resolveErr != nil {
		return messages, fmt.Errorf("resolving summary model %q: %w", summaryQualifiedModel, resolveErr)
	}
	summaryText := formatStructuredSummary(
		summarizeMessagesWithFallback(
			ctx,
			summaryClient,
			summaryBareModel,
			toSummarize,
			contextWindow,
			prompts.StructuredSummaryDefaultFocus,
		),
	)

	// Persist summary to conversation.
	summaryMessage := conversations.NewSummaryMessage(summaryText, time.Now().UnixMilli())
	store := self.ConversationsForUser(UserIDFromContext(ctx))
	if store != nil {
		if appendError := store.Append(conversationId, summaryMessage); appendError != nil {
			log.Debugf("failed to persist context summary: %v", appendError)
		}
	}

	// Build compressed messages: system prompt + summary + kept messages.
	compressed := make([]providers.ChatMessage, 0, 2+len(messages)-keepIdx)
	compressed = append(compressed, messages[0]) // system prompt
	compressed = append(compressed, providers.ChatMessage{
		Role:    "system",
		Content: prompts.PreviousConversationSummaryPrefix + summaryText,
	})
	compressed = append(compressed, messages[keepIdx:]...)

	log.Debugf("context compressed: %d messages -> %d messages (dropped %d, kept %d)",
		len(messages), len(compressed), len(toSummarize), len(messages)-keepIdx)

	return compressed, nil
}
