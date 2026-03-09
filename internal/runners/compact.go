package runners

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
)

const defaultContextWindow = 128000
const defaultSummaryChunkTokens = 12000
const defaultSummaryMaxMessageCharacters = 2000
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

type contextCompressionLimits struct {
	CompressionThreshold float64
	MinKeepMessages      int
	MinKeepRecentTokens  int
}

// CompactResult holds the outcome of a conversation compaction.
type CompactResult struct {
	SummarizedMessages int `json:"summarizedMessages"`
	SummaryLength      int `json:"summaryLength"`
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
	trimmedText := text
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

// estimateToolDefinitionsTokens estimates the token overhead of tool definitions.
func estimateToolDefinitionsTokens(tools []providers.ToolDefinition) int {
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

func trimToolResultText(text string, maxCharacters int) string {
	if maxCharacters <= 0 || len(text) <= maxCharacters {
		return text
	}
	headCharacters := int(float64(maxCharacters) * 0.75)
	if headCharacters <= 0 {
		headCharacters = maxCharacters / 2
	}
	if headCharacters >= maxCharacters {
		headCharacters = maxCharacters / 2
	}
	tailCharacters := maxCharacters - headCharacters
	if tailCharacters < 0 {
		tailCharacters = 0
	}
	head := text[:headCharacters]
	tail := ""
	if tailCharacters > 0 {
		tail = text[len(text)-tailCharacters:]
	}
	return fmt.Sprintf("%s\n...\n%s\n... (truncated)", head, tail)
}

// truncateOldToolResults applies a two-tier pruning strategy for old tool results.
func truncateOldToolResults(messages []providers.ChatMessage, minKeep int, maxCharacters int) []providers.ChatMessage {
	if len(messages) <= minKeep {
		return messages
	}
	boundary := len(messages) - minKeep

	result := make([]providers.ChatMessage, len(messages))
	copy(result, messages)
	hardLimitCharacters := maxCharacters * defaultHardClearToolMultiplier
	for index := 0; index < boundary; index++ {
		text, ok := result[index].Content.(string)
		if !ok || result[index].Role != "tool" {
			continue
		}
		if maxCharacters > 0 && len(text) > hardLimitCharacters {
			result[index].Content = defaultHardClearedToolPlaceholder
			continue
		}
		if maxCharacters > 0 && len(text) > maxCharacters {
			result[index].Content = trimToolResultText(text, maxCharacters)
		}
	}
	return result
}

// truncateAllToolResults truncates all tool results in the message list, including
// recent ones. Used as a last resort when the context is still too large after
// normal compression.
func truncateAllToolResults(messages []providers.ChatMessage, maxCharacters int) []providers.ChatMessage {
	result := make([]providers.ChatMessage, len(messages))
	copy(result, messages)
	hardLimitCharacters := maxCharacters * defaultHardClearToolMultiplier
	for index := range result {
		text, ok := result[index].Content.(string)
		if !ok || result[index].Role != "tool" {
			continue
		}
		if maxCharacters > 0 && len(text) > hardLimitCharacters {
			result[index].Content = defaultHardClearedToolPlaceholder
			continue
		}
		if maxCharacters > 0 && len(text) > maxCharacters {
			result[index].Content = trimToolResultText(text, maxCharacters)
		}
	}
	return result
}

// findKeepBoundary walks backward from the target split point to find an index
// where we can safely split without breaking tool call/result pairs.
func findKeepBoundary(messages []providers.ChatMessage, minKeep int) int {
	if len(messages) <= minKeep {
		return 0
	}
	target := len(messages) - minKeep

	// Walk backward from target to find a safe split point.
	index := target
	for index > 0 {
		if messages[index].Role == "tool" {
			for index > 0 && messages[index].Role == "tool" {
				index--
			}
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

// expandKeepBoundaryForRecentTokens moves the keep boundary earlier (smaller index)
// until at least minKeepRecentTokens are preserved in the tail.
func expandKeepBoundaryForRecentTokens(messages []providers.ChatMessage, keepIndex int, minKeepRecentTokens int) int {
	if minKeepRecentTokens <= 0 {
		return keepIndex
	}
	if keepIndex < 1 || keepIndex >= len(messages) {
		return keepIndex
	}
	keptTokens := 0
	for index := keepIndex; index < len(messages); index++ {
		keptTokens += estimateMessageTokens(messages[index])
	}
	for keepIndex > 1 && keptTokens < minKeepRecentTokens {
		keepIndex--
		keptTokens += estimateMessageTokens(messages[keepIndex])
	}
	return keepIndex
}

// findLastSummaryIndex returns the index of the last context_summary message
// in history, or -1 if none exists.
func findLastSummaryIndex(messages []*models.ConversationMessage) int {
	for messageIndex := len(messages) - 1; messageIndex >= 0; messageIndex-- {
		if conversationMessageRole(*messages[messageIndex]) == "system" &&
			conversationMessageStopReason(*messages[messageIndex]) == "context_summary" {
			return messageIndex
		}
	}
	return -1
}

// chatMessagesText builds a truncated text representation of provider chat messages.
func chatMessagesText(messages []providers.ChatMessage, maxTotalCharacters int, maxMessageCharacters int) string {
	var lines []string
	totalCharacters := 0

	for messageIndex := len(messages) - 1; messageIndex >= 0; messageIndex-- {
		role := messages[messageIndex].Role
		if role == "tool" && messages[messageIndex].Name != "" {
			role = fmt.Sprintf("tool(%s)", messages[messageIndex].Name)
		}

		content := messages[messageIndex].ContentText()
		if messages[messageIndex].Role == "tool" {
			content = sanitizeToolResultForCompaction(content)
		}
		if maxMessageCharacters > 0 && len(content) > maxMessageCharacters {
			content = content[:maxMessageCharacters] + "..."
		}

		line := fmt.Sprintf("[%s]: %s\n", role, content)
		if maxTotalCharacters > 0 && totalCharacters+len(line) > maxTotalCharacters {
			remaining := maxTotalCharacters - totalCharacters
			if remaining > 0 {
				lines = append(lines, line[:remaining])
			}
			break
		}
		lines = append(lines, line)
		totalCharacters += len(line)
	}

	// Reverse to chronological order.
	for leftIndex, rightIndex := 0, len(lines)-1; leftIndex < rightIndex; leftIndex, rightIndex = leftIndex+1, rightIndex-1 {
		lines[leftIndex], lines[rightIndex] = lines[rightIndex], lines[leftIndex]
	}

	return strings.Join(lines, "")
}

func normalizeFactLines(lines []string) []string {
	seen := make(map[string]struct{}, len(lines))
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmedLine := line
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
	return builder.String()
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
	if mergedSummary.Summary == "" {
		mergedSummary.Summary = baseSummary.Summary
	}
	return normalizeStructuredSummary(mergedSummary)
}

func parseStructuredSummaryResponse(rawText string) structuredSummary {
	trimmedText := rawText
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
			candidate := trimmedText[startIndex : endIndex+1]
			if json.Unmarshal([]byte(candidate), &parsedSummary) == nil {
				return normalizeStructuredSummary(parsedSummary)
			}
		}
	}

	return structuredSummary{Summary: rawText}
}

func summarizeChunk(
	ctx context.Context,
	provider providers.ChatProvider,
	modelName string,
	chunkText string,
	previousSummary string,
	focus string,
) (structuredSummary, error) {
	if chunkText == "" {
		return structuredSummary{}, nil
	}

	userPrompt := prompts.BuildStructuredSummaryUserPrompt(previousSummary, focus, chunkText)

	summaryRequest := providers.ChatRequest{
		ModelName: modelName,
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
	content := response.Choices[0].Message.ContentText()
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
	provider providers.ChatProvider,
	modelName string,
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
		chunkText := chatMessagesText(chunk, 0, defaultSummaryMaxMessageCharacters)
		chunkSummary, err := summarizeChunk(ctx, provider, modelName, chunkText, formatStructuredSummary(mergedSummary), focus)
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
	fallbackText := chatMessagesText(tailMessages, 4000, 500)
	if fallbackText == "" {
		fallbackText = "Recent context unavailable."
	}
	return structuredSummary{
		Summary: "Context summary degraded due to size limits. Preserved recent turns:\n" + fallbackText,
	}
}

func summarizeMessagesWithFallback(
	ctx context.Context,
	provider providers.ChatProvider,
	modelName string,
	messages []providers.ChatMessage,
	contextWindow int,
	focus string,
) structuredSummary {
	fullSummary, err := summarizeMessagesInStages(ctx, provider, modelName, messages, contextWindow, focus)
	if err == nil && fullSummary.Summary != "" {
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
		partialSummary, partialErr := summarizeMessagesInStages(ctx, provider, modelName, filteredMessages, contextWindow, focus)
		if partialErr == nil && partialSummary.Summary != "" {
			return partialSummary
		}
		log.Debugf("summary partial pass failed, falling back to deterministic summary: %v", partialErr)
	}

	return buildLastMessagesFallback(messages)
}

// summarizeAndPersist resolves the summarizer model, summarizes the given
// messages, persists the summary to the conversation, and returns the summary
// text. This is the shared core of CompactConversation and compressContext.
func (self *Runner) summarizeAndPersist(
	ctx context.Context,
	messages []providers.ChatMessage,
	contextWindow int,
) (string, error) {
	var configuration *models.Configuration
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		loadedConfiguration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		configuration = loadedConfiguration
		return nil
	}); err != nil {
		log.Debugf("runner: failed to load configuration from store: %v", err)
		return "", fmt.Errorf("failed to load configuration from store: %w", err)
	}
	providerModelName := ""
	if configuration != nil && configuration.Models != nil {
		if summarizerProviderModelName := configuration.Models.GetSummarizerProviderModelName(); summarizerProviderModelName != "" {
			providerModelName = summarizerProviderModelName
		}
	}
	resolved, _, modelName, err := self.providerRegistry.ResolveProviderAndModel(providerModelName)
	if err != nil {
		return "", fmt.Errorf("failed to resolve summary model %q: %w", providerModelName, err)
	}
	provider, ok := resolved.(providers.ChatProvider)
	if !ok {
		return "", fmt.Errorf("provider does not support chat for summarization")
	}

	summaryText := formatStructuredSummary(
		summarizeMessagesWithFallback(
			ctx,
			provider,
			modelName,
			messages,
			contextWindow,
			prompts.StructuredSummaryDefaultFocus,
		),
	)

	summaryMessage := newTextMessage("system", summaryText)
	stopReason := models.StopReason("context_summary")
	summaryMessage.StopReason = &stopReason
	if appendError := self.appendConversationMessage(ctx, summaryMessage); appendError != nil {
		return "", fmt.Errorf("saving summary: %w", appendError)
	}

	return summaryText, nil
}

// CompactConversation summarizes all messages in a conversation using the
// runner's buildMessages pipeline, reusing the cached system prompt and tool
// definitions. This enables prompt cache hits when the summarizer model matches
// the main model.
func (self *Runner) CompactConversation(ctx context.Context) (*CompactResult, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, fmt.Errorf("userId is required")
	}

	// Load conversation history.
	history, err := listConversationMessages(ctx, self.ConversationID)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("conversation is empty")
	}

	// Build messages via the same pipeline used for normal runs.
	llmMessages := self.buildMessages(ctx, history, SystemPromptModeFull, self.skillPrompts)

	summaryText, err := self.summarizeAndPersist(ctx, llmMessages, self.resolveContextWindow(ctx))
	if err != nil {
		return nil, err
	}

	log.Debugf("conversation compacted (cache-aware): %d messages summarized", len(history))

	return &CompactResult{
		SummarizedMessages: len(history),
		SummaryLength:      len(summaryText),
	}, nil
}

func (self *Runner) resolveContextWindow(ctx context.Context) int {
	var configuration *models.Configuration
	_ = store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		var err error
		configuration, err = transaction.GetConfiguration(ctx, nil)
		return err
	})
	contextWindow := 0
	if configuration != nil && configuration.Models != nil {
		contextWindow = configuration.Models.GetContextWindow()
	}
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	return contextWindow
}

// compressContext checks whether the estimated token count exceeds the
// compression threshold and, if so, summarizes older messages via an LLM call.
func (self *Runner) compressContext(
	ctx context.Context,
	messages []providers.ChatMessage,
	toolDefinitions []providers.ToolDefinition,
	limits contextCompressionLimits,
) ([]providers.ChatMessage, error) {
	contextWindow := self.resolveContextWindow(ctx)

	// Estimate total tokens.
	total := estimateToolDefinitionsTokens(toolDefinitions)
	for _, message := range messages {
		total += estimateMessageTokens(message)
	}

	threshold := int(float64(contextWindow) * limits.CompressionThreshold)
	if total <= threshold {
		return messages, nil
	}

	log.Debugf("context compression triggered: estimated %d tokens, threshold %d", total, threshold)

	// Find the split point.
	keepIndex := findKeepBoundary(messages[1:], limits.MinKeepMessages) + 1
	keepIndex = expandKeepBoundaryForRecentTokens(messages, keepIndex, limits.MinKeepRecentTokens)
	if keepIndex <= 1 {
		return messages, nil
	}

	// Messages to summarize: messages[1:keepIndex] (skip system prompt at 0).
	toSummarize := messages[1:keepIndex]

	summaryText, err := self.summarizeAndPersist(ctx, toSummarize, contextWindow)
	if err != nil {
		log.Debugf("failed to persist context summary: %v", err)
		return messages, nil
	}

	// Build compressed messages: system prompt + summary + kept messages.
	compressed := make([]providers.ChatMessage, 0, 2+len(messages)-keepIndex)
	compressed = append(compressed, messages[0]) // system prompt
	compressed = append(compressed, providers.ChatMessage{
		Role:    "system",
		Content: prompts.PreviousConversationSummaryPrefix + summaryText,
	})
	compressed = append(compressed, messages[keepIndex:]...)

	log.Debugf("context compressed: %d messages -> %d messages (dropped %d, kept %d)",
		len(messages), len(compressed), len(toSummarize), len(messages)-keepIndex)

	return compressed, nil
}
