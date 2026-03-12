package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type pipelinesTool struct {
	binary string
	runner commandRunner
}

func (self *pipelinesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitlab_pipelines",
			Description: "Interact with GitLab CI/CD pipelines. Actions: list (list pipelines), " +
				"view (view pipeline details), run (trigger a pipeline), retry (retry a pipeline).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "run", "retry"},
						"description": "The pipelines action to perform.",
					},
					"pipeline_id": map[string]interface{}{
						"type":        "string",
						"description": "Pipeline ID (for view and retry actions).",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Branch name to run pipeline on (for run action).",
					},
					"per_page": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results per page (for list action, default 30).",
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
				"description": "JSON object with pipeline data from GitLab.",
			},
		},
	}
}

func (self *pipelinesTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *pipelinesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		PipelineID string `json:"pipeline_id"`
		Branch     string `json:"branch"`
		PerPage    int    `json:"per_page"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		perPage := args.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArgs := []string{"ci", "list",
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		appendRepository(&commandArgs, args.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "view":
		if args.PipelineID == "" {
			return "", fmt.Errorf("pipeline_id is required for view action")
		}
		commandArgs := []string{"ci", "get",
			"--pipeline-id", args.PipelineID,
			"--output", "json"}
		appendRepository(&commandArgs, args.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "run":
		commandArgs := []string{"ci", "run"}
		if args.Branch != "" {
			commandArgs = append(commandArgs, "--branch", args.Branch)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("triggered", output), nil

	case "retry":
		if args.PipelineID == "" {
			return "", fmt.Errorf("pipeline_id is required for retry action")
		}
		commandArgs := []string{"ci", "retry", args.PipelineID}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("retried", output), nil

	default:
		return "", fmt.Errorf("unknown pipelines action: %s", args.Action)
	}
}
