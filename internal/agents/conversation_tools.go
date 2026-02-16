package agents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterConversationTools adds conversation-related tools to the registry.
func RegisterConversationTools(registry *ToolRegistry, conversations *conversations.Store) {
	registry.Register(&listConversationsTool{conversations: conversations})
}

// --- conversation_list ---

type listConversationsTool struct {
	conversations *conversations.Store
}

func (self *listConversationsTool) Definition() provider.ToolDef {
	return provider.ToolDef{
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
