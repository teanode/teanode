package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type reposTool struct {
	binary string
	runner commandRunner
}

func (self *reposTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitea_repos",
			Description: "Interact with Gitea repositories. Actions: view (get repo info), " +
				"list (list your repos), search (search for repos).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"view", "list", "search"},
						"description": "The repos action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search term (for search action).",
					},
					"type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"fork", "mirror", "source"},
						"description": "Filter by repository type (for list and search actions).",
					},
					"owner": map[string]interface{}{
						"type":        "string",
						"description": "Filter by owner (for search action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for list and search actions, default 30).",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Target repository in owner/repo format (for view action). If omitted, uses the current repository context.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with repository data from Gitea.",
			},
		},
	}
}

func (self *reposTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *reposTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Query      string `json:"query"`
		Type       string `json:"type"`
		Owner      string `json:"owner"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "view":
		commandArgs := []string{"repos", "--output", "json"}
		if args.Repository != "" {
			commandArgs = append(commandArgs, args.Repository)
		}
		return execGitea(ctx, self.runner, self.binary, commandArgs...)

	case "list":
		limit := args.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArgs := []string{"repos", "list",
			"--output", "json",
			"--limit", strconv.Itoa(limit)}
		if args.Type != "" {
			commandArgs = append(commandArgs, "--type", args.Type)
		}
		return execGitea(ctx, self.runner, self.binary, commandArgs...)

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArgs := []string{"repos", "search",
			"--output", "json",
			"--limit", strconv.Itoa(limit),
			args.Query}
		if args.Type != "" {
			commandArgs = append(commandArgs, "--type", args.Type)
		}
		if args.Owner != "" {
			commandArgs = append(commandArgs, "--owner", args.Owner)
		}
		return execGitea(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown repos action: %s", args.Action)
	}
}
