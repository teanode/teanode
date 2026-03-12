package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

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

func (self *projectsTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *projectsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Query      string `json:"query"`
		PerPage    int    `json:"per_page"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "view":
		commandArgs := []string{"repo", "view", "--output", "json"}
		appendRepository(&commandArgs, args.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "list":
		perPage := args.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArgs := []string{"repo", "list",
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		perPage := args.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArgs := []string{"repo", "search",
			"--search", args.Query,
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown projects action: %s", args.Action)
	}
}
