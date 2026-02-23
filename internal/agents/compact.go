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
// everything before it. This is the standalone version that builds a fresh
// summarization request (no prompt cache reuse).
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
	if err != nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.ContentText()) == "" {
		summaryText = fmt.Sprintf("[Earlier conversation with %d messages was dropped due to compaction]", len(messages))
	} else {
		summaryText = strings.TrimSpace(response.Choices[0].Message.ContentText())
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

// CompactConversation summarizes all messages in a conversation using the
// runner's buildMessages pipeline, reusing the cached system prompt and tool
// definitions. This enables prompt cache hits when the summarizer model matches
// the main model.
func (self *Runner) CompactConversation(ctx context.Context, conversationId string) (*CompactResult, error) {
	configuration, providerRegistry, tools, workspaceDirectory, skillPrompts := self.Snapshot()
	userId := UserIDFromContext(ctx)
	if strings.TrimSpace(userId) == "" {
		return nil, fmt.Errorf("userId is required")
	}
	if self.ResolveUserProfile == nil {
		return nil, fmt.Errorf("ResolveUserProfile is required")
	}
	profile, err := self.ResolveUserProfile(userId)
	if err != nil {
		return nil, fmt.Errorf("resolving user profile for %q: %w", userId, err)
	}
	if profile == nil {
		return nil, fmt.Errorf("user profile is required for user %q", userId)
	}
	store := self.ConversationsForUser(userId)
	if store == nil {
		return nil, fmt.Errorf("conversation store is not configured")
	}

	// Load conversation history.
	history, err := store.Load(conversationId)
	if err != nil {
		return nil, fmt.Errorf("loading conversation: %w", err)
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("conversation is empty")
	}

	qualifiedModel := configuration.AgentModel(self.AgentID)
	if header, headerErr := store.LoadHeader(conversationId); headerErr == nil && header.Model != "" {
		qualifiedModel = header.Model
	}
	limits := configuration.ResolveModelLimits(qualifiedModel)
	userWorkspaceDirectory := ""
	if resolvedUserWorkspaceDirectory, resolveErr := configs.UserWorkspaceDirectory(userId); resolveErr == nil {
		userWorkspaceDirectory = resolvedUserWorkspaceDirectory
	}

	// Build messages via the same pipeline used for normal runs.
	llmMessages := self.buildMessages(history, limits, "", configuration, userId, workspaceDirectory, userWorkspaceDirectory, skillPrompts, profile)

	// Build tool definitions.
	var toolDefs []providers.ToolDefinition
	if tools != nil {
		toolDefs = tools.Definitions()
	}

	// Append summarization instruction.
	llmMessages = append(llmMessages, providers.ChatMessage{
		Role:    "user",
		Content: "Summarize the preceding conversation into a concise summary (max 500 words). Preserve key facts, decisions, tool results, and user preferences. Focus on information needed to continue the conversation naturally.",
	})

	// Resolve summarizer model.
	qualifiedModel = configuration.Models.Default
	if configuration.Models.SummarizerModel != "" {
		qualifiedModel = configuration.Models.SummarizerModel
	}

	provider, bareModel, err := providerRegistry.Resolve(qualifiedModel)
	if err != nil {
		return nil, fmt.Errorf("resolving summary model %q: %w", qualifiedModel, err)
	}

	summaryRequest := providers.ChatRequest{
		Model:    bareModel,
		Messages: llmMessages,
		Tools:    toolDefs,
	}

	var summaryText string
	response, err := provider.ChatCompletion(ctx, summaryRequest)
	if err != nil || len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.ContentText()) == "" {
		summaryText = fmt.Sprintf("[Earlier conversation with %d messages was dropped due to compaction]", len(history))
	} else {
		summaryText = strings.TrimSpace(response.Choices[0].Message.ContentText())
	}

	// Persist summary to conversation.
	summaryMessage := conversations.NewSummaryMessage(summaryText, time.Now().UnixMilli())
	if err := store.Append(conversationId, summaryMessage); err != nil {
		return nil, fmt.Errorf("saving summary: %w", err)
	}

	log.Debugf("conversation compacted (cache-aware): %d messages summarized", len(history))

	return &CompactResult{
		SummarizedMessages: len(history),
		SummaryLength:      len(summaryText),
	}, nil
}
