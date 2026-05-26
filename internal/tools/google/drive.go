package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type driveTool struct {
	binary string

	runner commandRunner
}

func (self *driveTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "google_drive",
			Description: "Interact with Google Drive. Actions: list (recent files), search (find files), " +
				"info (file details).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "search", "info"},
						"description": "The Drive action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action).",
					},
					"file_id": map[string]interface{}{
						"type":        "string",
						"description": "File ID (for 'info' action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for 'list' and 'search' actions, default 10).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *driveTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *driveTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action          string `json:"action"`
		Query           string `json:"query"`
		WorkspaceFileID string `json:"file_id"`
		Limit           int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("google: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		limit := arguments.Limit
		if limit <= 0 {
			limit = 10
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"drive", "ls", "--max", strconv.Itoa(limit))

	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("google: query is required for search action")
		}
		commandArguments := []string{"drive", "search", arguments.Query}
		if arguments.Limit > 0 {
			commandArguments = append(commandArguments, "--max", strconv.Itoa(arguments.Limit))
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account, commandArguments...)

	case "info":
		if arguments.WorkspaceFileID == "" {
			return "", fmt.Errorf("google: file_id is required for info action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"drive", "get", arguments.WorkspaceFileID)

	default:
		return "", fmt.Errorf("google: unknown drive action: %s", arguments.Action)
	}
}
