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

type projectsTool struct {
	binary string
	runner commandRunner
}

func (self *projectsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitlab_projects",
			Description: "Interact with GitLab projects. Actions: view (get project info), " +
				"list (list your projects), search (search for projects).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"view", "list", "search"},
						"description": "The projects action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for search action).",
					},
					"per_page": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results per page (for list and search actions, default 30).",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Target project in OWNER/REPO or GROUP/NAMESPACE/REPO format (for view action). If omitted, uses the current repository context.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with project data from GitLab.",
			},
		},
	}
}

func (self *projectsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *projectsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		Query      string `json:"query"`
		PerPage    int    `json:"per_page"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitlab: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "view":
		commandArguments := []string{"repo", "view", "--output", "json"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "list":
		perPage := arguments.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArguments := []string{"repo", "list",
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("gitlab: query is required for search action")
		}
		perPage := arguments.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArguments := []string{"repo", "search",
			"--search", arguments.Query,
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	default:
		return "", fmt.Errorf("gitlab: unknown projects action: %s", arguments.Action)
	}
}
