package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/provider"
)

type contactsTool struct {
	binary  string
	account string
	runner  commandRunner
}

func (self *contactsTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "google_contacts",
			Description: "Interact with Google Contacts. Actions: search (find contacts), list (all contacts).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"search", "list"},
						"description": "The Contacts action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default 10).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *contactsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		cmdArgs := []string{"contacts", "search", "--query", args.Query}
		if args.Limit > 0 {
			cmdArgs = append(cmdArgs, "--limit", strconv.Itoa(args.Limit))
		}
		return execGog(ctx, self.runner, self.binary, self.account, cmdArgs...)

	case "list":
		cmdArgs := []string{"contacts", "list"}
		if args.Limit > 0 {
			cmdArgs = append(cmdArgs, "--limit", strconv.Itoa(args.Limit))
		}
		return execGog(ctx, self.runner, self.binary, self.account, cmdArgs...)

	default:
		return "", fmt.Errorf("unknown contacts action: %s", args.Action)
	}
}
