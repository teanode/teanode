package mattermost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type usersTool struct {
	binary string
	runner commandRunner
}

func (self *usersTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "mattermost_users",
			Description: "Interact with Mattermost users. Actions: me (show your profile), " +
				"info (show user profile), status (get or set your status), " +
				"search (search for users), list (list team members), autocomplete.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"me", "info", "status", "search", "list", "autocomplete"},
						"description": "The user action to perform.",
					},
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username (for 'info' action).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query or prefix (for 'search' and 'autocomplete' actions).",
					},
					"status_value": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"online", "away", "dnd", "offline"},
						"description": "Status to set (for 'status' action). Omit to get current status.",
					},
					"status_message": map[string]interface{}{
						"type":        "string",
						"description": "Custom status text (for 'status' action).",
					},
					"status_emoji": map[string]interface{}{
						"type":        "string",
						"description": "Custom status emoji (for 'status' action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for 'list' action, default 50).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *usersTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"me", "info", "search", "list", "autocomplete"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *usersTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action        string `json:"action"`
		Username      string `json:"username"`
		Query         string `json:"query"`
		StatusValue   string `json:"status_value"`
		StatusMessage string `json:"status_message"`
		StatusEmoji   string `json:"status_emoji"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("mattermost: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "me":
		return execMattermost(ctx, self.runner, self.binary, "user", "me")

	case "info":
		if arguments.Username == "" {
			return "", fmt.Errorf("mattermost: username is required for info action")
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "info", arguments.Username)

	case "status":
		commandArguments := []string{"user", "status"}
		if arguments.StatusValue != "" {
			commandArguments = append(commandArguments, arguments.StatusValue)
		}
		if arguments.StatusMessage != "" {
			commandArguments = append(commandArguments, "--message", arguments.StatusMessage)
		}
		if arguments.StatusEmoji != "" {
			commandArguments = append(commandArguments, "--emoji", arguments.StatusEmoji)
		}
		return execMattermost(ctx, self.runner, self.binary, commandArguments...)

	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("mattermost: query is required for search action")
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "search", arguments.Query)

	case "list":
		limit := arguments.Limit
		if limit <= 0 {
			limit = 50
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "list",
			"-n", fmt.Sprintf("%d", limit))

	case "autocomplete":
		if arguments.Query == "" {
			return "", fmt.Errorf("mattermost: query is required for autocomplete action")
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "autocomplete", arguments.Query)

	default:
		return "", fmt.Errorf("mattermost: unknown users action: %s", arguments.Action)
	}
}
