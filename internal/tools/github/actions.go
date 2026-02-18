package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
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

func (self *actionsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		RunID      string `json:"run_id"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list_workflows":
		commandArgs := []string{"workflow", "list",
			"--json", "id,name,state"}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "list_runs":
		limit := args.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArgs := []string{"run", "list",
			"--json", "databaseId,displayTitle,status,conclusion,headBranch,createdAt",
			"--limit", strconv.Itoa(limit)}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "view_run":
		if args.RunID == "" {
			return "", fmt.Errorf("run_id is required for view_run action")
		}
		commandArgs := []string{"run", "view", args.RunID,
			"--json", "databaseId,displayTitle,status,conclusion,jobs"}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown actions action: %s", args.Action)
	}
}
