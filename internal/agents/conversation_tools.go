package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterConversationTools adds conversation-related tools to the registry.
func RegisterConversationTools(registry *ToolRegistry, store *conversations.Store, providers *provider.Registry, configuration *configs.Config) {
	registry.Register(&listConversationsTool{conversations: store})
	registry.Register(&compactConversationTool{
		conversations: store,
		providers:     providers,
		config:        configuration,
	})
}

// --- conversation_list ---

type listConversationsTool struct {
	conversations *conversations.Store
}

func (self *listConversationsTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
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

func (self *listConversationsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Limit int `json:"limit"`
	}
	if rawArguments != "" {
		json.Unmarshal([]byte(rawArguments), &arguments)
	}
	if arguments.Limit <= 0 {
		arguments.Limit = 10
	}

	currentConversationId := ConversationIDFromContext(ctx)

	allConversations, err := self.conversations.List()
	if err != nil {
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
		filtered = append(filtered, conversationEntry{
			ID:         conversationInfo.ID,
			Title:      conversationInfo.Title,
			Summary:    conversationInfo.Summary,
			LastActive: conversationInfo.LastActive,
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
	conversations *conversations.Store
	providers     *provider.Registry
	config        *configs.Config
}

func (self *compactConversationTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
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
					"keptMessages": map[string]interface{}{
						"type":        "integer",
						"description": "Number of recent messages preserved.",
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

func (self *compactConversationTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	conversationId := ConversationIDFromContext(ctx)
	if conversationId == "" {
		return "", fmt.Errorf("no active conversation")
	}

	compactResult, err := CompactConversation(ctx, self.conversations, self.providers, self.config, conversationId, 0)
	if err != nil {
		return "", err
	}

	result, _ := json.Marshal(compactResult)
	return string(result), nil
}
