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

func (self *reposTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *reposTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		Owner      string `json:"owner"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("github: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "view":
		commandArguments := []string{"repo", "view"}
		if arguments.Repository != "" {
			commandArguments = append(commandArguments, arguments.Repository)
		}
		commandArguments = append(commandArguments,
			"--json", "name,description,url,defaultBranchRef,stargazerCount,isPrivate,forkCount")
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "list":
		if arguments.Owner == "" {
			return "", fmt.Errorf("github: owner is required for list action")
		}
		limit := arguments.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArguments := []string{"repo", "list", arguments.Owner,
			"--json", "name,description,isPrivate,updatedAt",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	default:
		return "", fmt.Errorf("github: unknown repos action: %s", arguments.Action)
	}
}
