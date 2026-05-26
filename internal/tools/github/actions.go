package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type actionsTool struct {
	binary string
	runner commandRunner
}

func (self *actionsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "github_actions",
			Description: "Interact with GitHub Actions. Actions: list_workflows (list workflows), " +
				"list_runs (list workflow runs), view_run (view run details).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list_workflows", "list_runs", "view_run"},
						"description": "The Actions action to perform.",
					},
					"run_id": map[string]interface{}{
						"type":        "string",
						"description": "Workflow run ID (for view_run action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for list_runs action, default 30).",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Target repository in owner/repo format. If omitted, uses the current repository context.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with GitHub Actions data.",
			},
		},
	}
}

func (self *actionsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list_workflows", "list_runs", "view_run"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *actionsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		RunID      string `json:"run_id"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("github: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list_workflows":
		commandArguments := []string{"workflow", "list",
			"--json", "id,name,state"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "list_runs":
		limit := arguments.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArguments := []string{"run", "list",
			"--json", "databaseId,displayTitle,status,conclusion,headBranch,createdAt",
			"--limit", strconv.Itoa(limit)}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "view_run":
		if arguments.RunID == "" {
			return "", fmt.Errorf("github: run_id is required for view_run action")
		}
		commandArguments := []string{"run", "view", arguments.RunID,
			"--json", "databaseId,displayTitle,status,conclusion,jobs"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	default:
		return "", fmt.Errorf("github: unknown actions action: %s", arguments.Action)
	}
}
