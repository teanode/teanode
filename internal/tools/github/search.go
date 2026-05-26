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

type searchTool struct {
	binary string
	runner commandRunner
}

func (self *searchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "github_search",
			Description: "Search GitHub. Actions: issues (search issues), pulls (search pull requests), " +
				"code (search code). Use repo:owner/name qualifiers in the query to scope results.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"issues", "pulls", "code"},
						"description": "The search action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query. Use GitHub search qualifiers like repo:owner/name, language:go, etc.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default 30).",
					},
				},
				"required": []string{"action", "query"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON array of search results from GitHub.",
			},
		},
	}
}

func (self *searchTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *searchTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("github: parsing arguments: %w", err)
	}

	if arguments.Query == "" {
		return "", fmt.Errorf("github: query is required for search action")
	}

	limit := arguments.Limit
	if limit <= 0 {
		limit = 30
	}

	switch arguments.Action {
	case "issues":
		commandArguments := []string{"search", "issues", arguments.Query,
			"--json", "number,title,state,repository",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "pulls":
		commandArguments := []string{"search", "prs", arguments.Query,
			"--json", "number,title,state,repository",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "code":
		commandArguments := []string{"search", "code", arguments.Query,
			"--json", "path,repository,textMatches",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	default:
		return "", fmt.Errorf("github: unknown search action: %s", arguments.Action)
	}
}
