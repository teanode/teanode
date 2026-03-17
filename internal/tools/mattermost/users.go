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
				"search (search for users), list (list team members).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"me", "info", "status", "search", "list"},
						"description": "The user action to perform.",
					},
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username (for 'info' action).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action).",
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
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"me", "info", "search", "list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *usersTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action        string `json:"action"`
		Username      string `json:"username"`
		Query         string `json:"query"`
		StatusValue   string `json:"status_value"`
		StatusMessage string `json:"status_message"`
		StatusEmoji   string `json:"status_emoji"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "me":
		return execMattermost(ctx, self.runner, self.binary, "user", "me")

	case "info":
		if args.Username == "" {
			return "", fmt.Errorf("username is required for info action")
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "info", args.Username)

	case "status":
		if args.StatusValue != "" {
			commandArgs := []string{"user", "status", args.StatusValue}
			if args.StatusMessage != "" {
				commandArgs = append(commandArgs, "--message", args.StatusMessage)
			}
			if args.StatusEmoji != "" {
				commandArgs = append(commandArgs, "--emoji", args.StatusEmoji)
			}
			return execMattermost(ctx, self.runner, self.binary, commandArgs...)
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "status")

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "search", args.Query)

	case "list":
		limit := args.Limit
		if limit <= 0 {
			limit = 50
		}
		return execMattermost(ctx, self.runner, self.binary, "user", "list",
			"-n", fmt.Sprintf("%d", limit))

	default:
		return "", fmt.Errorf("unknown users action: %s", args.Action)
	}
}
