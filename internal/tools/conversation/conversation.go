// Package conversation exposes tools for interacting with the active conversation.
package conversation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&listConversationsTool{}, &compactConversationTool{}}
	})
}

// --- conversation_list ---

type listConversationsTool struct {
}

func (self *listConversationsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "conversation_list",
			Description: "List other conversations for this agent. Returns conversation ids, titles, summaries, and last activity times. The current conversation is excluded from results.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of conversations to return. Defaults to 10.",
					},
				},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"conversations": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":         map[string]interface{}{"type": "string"},
								"title":      map[string]interface{}{"type": "string"},
								"summary":    map[string]interface{}{"type": "string"},
								"lastActive": map[string]interface{}{"type": "integer"},
							},
						},
					},
					"total": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of other conversations (before limit is applied).",
					},
				},
			},
		},
	}
}

func (self *listConversationsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *listConversationsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Limit int `json:"limit"`
	}
	if rawArguments != "" {
		if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if arguments.Limit <= 0 {
		arguments.Limit = 10
	}

	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", fmt.Errorf("runner context missing")
	}
	currentConversationId := runner.ConversationID
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("userId is required")
	}

	allConversations := make([]*models.Conversation, 0)
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		conversations, err := transaction.ListConversations(ctx, store.ConversationListOptions{
			UserID:  ptrto.Value(user.ID),
			AgentID: ptrto.Value(runner.AgentID),
		}, nil)
		if err != nil {
			return err
		}
		allConversations = append(allConversations, conversations...)
		return nil
	}); err != nil {
		return "", fmt.Errorf("listing conversations: %w", err)
	}

	// Filter out the current conversation.
	type conversationEntry struct {
		ID         string `json:"id"`
		Title      string `json:"title,omitempty"`
		Summary    string `json:"summary,omitempty"`
		LastActive int64  `json:"lastActive"`
	}

	var filtered []conversationEntry
	for _, conversationInfo := range allConversations {
		if conversationInfo.ID == currentConversationId {
			continue
		}
		lastActive := int64(0)
		if conversationInfo.ModifiedAt != nil {
			lastActive = conversationInfo.ModifiedAt.UnixMilli()
		} else if conversationInfo.CreatedAt != nil {
			lastActive = conversationInfo.CreatedAt.UnixMilli()
		}
		filtered = append(filtered, conversationEntry{
			ID:         conversationInfo.ID,
			Title:      conversationInfo.GetTitle(),
			Summary:    conversationInfo.GetSummary(),
			LastActive: lastActive,
		})
	}

	total := len(filtered)
	if len(filtered) > arguments.Limit {
		filtered = filtered[:arguments.Limit]
	}

	result, _ := json.Marshal(map[string]interface{}{
		"conversations": filtered,
		"total":         total,
	})
	return string(result), nil
}

// --- conversation_compact ---

type compactConversationTool struct {
}

func (self *compactConversationTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "conversation_compact",
			Description: "Compact the current conversation by summarizing older messages. Use when the conversation is getting long.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"summarizedMessages": map[string]interface{}{
						"type":        "integer",
						"description": "Number of messages that were summarized.",
					},
					"summaryLength": map[string]interface{}{
						"type":        "integer",
						"description": "Character length of the generated summary.",
					},
				},
			},
		},
	}
}

func (self *compactConversationTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *compactConversationTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", fmt.Errorf("runner context missing")
	}
	compactResult, err := runner.CompactConversation(ctx)
	if err != nil {
		return "", err
	}
	result, _ := json.Marshal(compactResult)
	return string(result), nil
}
