package gitlab

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/providers"
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

func (self *releasesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Tag        string `json:"tag"`
		Name       string `json:"name"`
		Notes      string `json:"notes"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		commandArgs := []string{"release", "list"}
		appendRepository(&commandArgs, args.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Tag == "" {
			return "", fmt.Errorf("tag is required for create action")
		}
		if args.Name == "" {
			return "", fmt.Errorf("name is required for create action")
		}
		commandArgs := []string{"release", "create", args.Tag,
			"--name", args.Name}
		if args.Notes != "" {
			commandArgs = append(commandArgs, "--notes", args.Notes)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	default:
		return "", fmt.Errorf("unknown releases action: %s", args.Action)
	}
}
