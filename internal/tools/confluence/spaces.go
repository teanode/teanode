package confluence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type spacesTool struct {
	binary string
	runner commandRunner
}

func (self *spacesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "confluence_spaces",
			Description: "Manage Confluence spaces. Actions: list (list all spaces).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list"},
						"description": "The space action to perform.",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *spacesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
	}
}

func (self *spacesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("confluence: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		return execConfluence(ctx, self.runner, self.binary, "spaces")

	default:
		return "", fmt.Errorf("confluence: unknown spaces action: %s", arguments.Action)
	}
}
