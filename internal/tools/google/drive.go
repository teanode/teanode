package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/provider"
)

type driveTool struct {
	binary  string
	account string
	runner  commandRunner
}

func (self *driveTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
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
		Action string `json:"action"`
		Query  string `json:"query"`
		FileID string `json:"file_id"`
		Limit  int    `json:"limit"`
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
		return execGog(ctx, self.runner, self.binary, self.account,
			"drive", "list", "--limit", strconv.Itoa(limit))

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		cmdArgs := []string{"drive", "search", "--query", args.Query}
		if args.Limit > 0 {
			cmdArgs = append(cmdArgs, "--limit", strconv.Itoa(args.Limit))
		}
		return execGog(ctx, self.runner, self.binary, self.account, cmdArgs...)

	case "info":
		if args.FileID == "" {
			return "", fmt.Errorf("file_id is required for info action")
		}
		return execGog(ctx, self.runner, self.binary, self.account,
			"drive", "info", args.FileID)

	default:
		return "", fmt.Errorf("unknown drive action: %s", args.Action)
	}
}
