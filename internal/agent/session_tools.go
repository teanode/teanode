package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ziyan/teanode/internal/provider"
	"github.com/ziyan/teanode/internal/session"
)

// RegisterSessionTools adds session-related tools to the registry.
func RegisterSessionTools(registry *ToolRegistry, sessions *session.Store) {
	registry.Register(&setTitleTool{sessions: sessions})
}

// --- set_title ---

type setTitleTool struct {
	sessions *session.Store
}

func (self *setTitleTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "set_title",
			Description: "Set the title of the current conversation session. Use this to give the session a meaningful, human-readable name.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title": map[string]interface{}{
						"type":        "string",
						"description": "The title to set for this session.",
					},
				},
				"required": []string{"title"},
			},
		},
	}
}

func (self *setTitleTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Title == "" {
		return "", fmt.Errorf("title is required")
	}

	sessionKey := SessionKeyFromContext(ctx)
	if sessionKey == "" {
		return "", fmt.Errorf("no active session")
	}

	if err := self.sessions.SetTitle(sessionKey, arguments.Title); err != nil {
		return "", fmt.Errorf("setting title: %w", err)
	}

	if callback := TitleCallbackFromContext(ctx); callback != nil {
		callback(arguments.Title)
	}

	return "ok", nil
}
