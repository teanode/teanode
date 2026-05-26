package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/models"
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

func (self *pipelinesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *pipelinesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		PipelineID string `json:"pipeline_id"`
		Branch     string `json:"branch"`
		PerPage    int    `json:"per_page"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitlab: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		perPage := arguments.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArguments := []string{"ci", "list",
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.PipelineID == "" {
			return "", fmt.Errorf("gitlab: pipeline_id is required for view action")
		}
		commandArguments := []string{"ci", "get",
			"--pipeline-id", arguments.PipelineID,
			"--output", "json"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "run":
		commandArguments := []string{"ci", "run"}
		if arguments.Branch != "" {
			commandArguments = append(commandArguments, "--branch", arguments.Branch)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("triggered", output), nil

	case "retry":
		if arguments.PipelineID == "" {
			return "", fmt.Errorf("gitlab: pipeline_id is required for retry action")
		}
		commandArguments := []string{"ci", "retry", arguments.PipelineID}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("retried", output), nil

	default:
		return "", fmt.Errorf("gitlab: unknown pipelines action: %s", arguments.Action)
	}
}
