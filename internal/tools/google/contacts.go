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

type contactsTool struct {
	binary string

	runner commandRunner
}

func (self *contactsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "google_contacts",
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

func (self *contactsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"search", "list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *contactsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("google: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("google: query is required for search action")
		}
		commandArguments := []string{"contacts", "search", arguments.Query}
		if arguments.Limit > 0 {
			commandArguments = append(commandArguments, "--max", strconv.Itoa(arguments.Limit))
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account, commandArguments...)

	case "list":
		commandArguments := []string{"contacts", "list"}
		if arguments.Limit > 0 {
			commandArguments = append(commandArguments, "--max", strconv.Itoa(arguments.Limit))
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account, commandArguments...)

	default:
		return "", fmt.Errorf("google: unknown contacts action: %s", arguments.Action)
	}
}
