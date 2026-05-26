package gitlab

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type releasesTool struct {
	binary string
	runner commandRunner
}

func (self *releasesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitlab_releases",
			Description: "Interact with GitLab releases. Actions: list (list releases), " +
				"create (create a new release).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "create"},
						"description": "The releases action to perform.",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Git tag for the release (for create action).",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Release name (for create action).",
					},
					"notes": map[string]interface{}{
						"type":        "string",
						"description": "Release notes (for create action).",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Target project in OWNER/REPO or GROUP/NAMESPACE/REPO format. If omitted, uses the current repository context.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with release data from GitLab.",
			},
		},
	}
}

func (self *releasesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *releasesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		Tag        string `json:"tag"`
		Name       string `json:"name"`
		Notes      string `json:"notes"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitlab: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		commandArguments := []string{"release", "list"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Tag == "" {
			return "", fmt.Errorf("gitlab: tag is required for create action")
		}
		if arguments.Name == "" {
			return "", fmt.Errorf("gitlab: name is required for create action")
		}
		commandArguments := []string{"release", "create", arguments.Tag,
			"--name", arguments.Name}
		if arguments.Notes != "" {
			commandArguments = append(commandArguments, "--notes", arguments.Notes)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	default:
		return "", fmt.Errorf("gitlab: unknown releases action: %s", arguments.Action)
	}
}
