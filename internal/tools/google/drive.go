package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
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

func (self *driveTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action          string `json:"action"`
		Query           string `json:"query"`
		WorkspaceFileID string `json:"file_id"`
		Limit           int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"drive", "ls", "--max", strconv.Itoa(limit))

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		commandArguments := []string{"drive", "search", args.Query}
		if args.Limit > 0 {
			commandArguments = append(commandArguments, "--max", strconv.Itoa(args.Limit))
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account, commandArguments...)

	case "info":
		if args.WorkspaceFileID == "" {
			return "", fmt.Errorf("file_id is required for info action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"drive", "get", args.WorkspaceFileID)

	default:
		return "", fmt.Errorf("unknown drive action: %s", args.Action)
	}
}
