package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
)

type reposTool struct {
	binary string
	runner commandRunner
}

func (self *reposTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "github_repos",
			Description: "Interact with GitHub repositories. Actions: view (get repository info), " +
				"list (list repositories for an owner).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"view", "list"},
						"description": "The repos action to perform.",
					},
					"owner": map[string]interface{}{
						"type":        "string",
						"description": "GitHub user or organization (for list action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for list action, default 30).",
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
				"description": "JSON object with repository data from GitHub.",
			},
		},
	}
}

func (self *reposTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Owner      string `json:"owner"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "view":
		commandArgs := []string{"repo", "view"}
		if args.Repository != "" {
			commandArgs = append(commandArgs, args.Repository)
		}
		commandArgs = append(commandArgs,
			"--json", "name,description,url,defaultBranchRef,stargazerCount,isPrivate,forkCount")
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "list":
		if args.Owner == "" {
			return "", fmt.Errorf("owner is required for list action")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArgs := []string{"repo", "list", args.Owner,
			"--json", "name,description,isPrivate,updatedAt",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown repos action: %s", args.Action)
	}
}
