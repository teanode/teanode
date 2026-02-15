package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/session"
)

// RegisterSessionTools adds session-related tools to the registry.
func RegisterSessionTools(registry *ToolRegistry, sessions *session.Store) {
	registry.Register(&listSessionsTool{sessions: sessions})
}

// --- list_sessions ---

type listSessionsTool struct {
	sessions *session.Store
}

func (self *listSessionsTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "session_list",
			Description: "List other conversation sessions for this agent. Returns session keys, titles, summaries, and last activity times. The current session is excluded from results.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of sessions to return. Defaults to 10.",
					},
				},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"sessions": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"key":        map[string]interface{}{"type": "string"},
								"title":      map[string]interface{}{"type": "string"},
								"summary":    map[string]interface{}{"type": "string"},
								"lastActive": map[string]interface{}{"type": "integer"},
							},
						},
					},
					"total": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of other sessions (before limit is applied).",
					},
				},
			},
		},
	}
}

func (self *listSessionsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Limit int `json:"limit"`
	}
	if rawArguments != "" {
		json.Unmarshal([]byte(rawArguments), &arguments)
	}
	if arguments.Limit <= 0 {
		arguments.Limit = 10
	}

	currentSessionKey := SessionKeyFromContext(ctx)

	allSessions, err := self.sessions.List()
	if err != nil {
		return "", fmt.Errorf("listing sessions: %w", err)
	}

	// Filter out the current session.
	type sessionEntry struct {
		Key        string `json:"key"`
		Title      string `json:"title,omitempty"`
		Summary    string `json:"summary,omitempty"`
		LastActive int64  `json:"lastActive"`
	}

	var filtered []sessionEntry
	for _, sessionInfo := range allSessions {
		if sessionInfo.Key == currentSessionKey {
			continue
		}
		filtered = append(filtered, sessionEntry{
			Key:        sessionInfo.Key,
			Title:      sessionInfo.Title,
			Summary:    sessionInfo.Summary,
			LastActive: sessionInfo.LastActive,
		})
	}

	total := len(filtered)
	if len(filtered) > arguments.Limit {
		filtered = filtered[:arguments.Limit]
	}

	result, _ := json.Marshal(map[string]interface{}{
		"sessions": filtered,
		"total":    total,
	})
	return string(result), nil
}
