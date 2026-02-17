package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/provider"
)

type searchTool struct {
	binary string
	runner commandRunner
}

func (self *searchTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
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

func (self *searchTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action string `json:"action"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	if args.Query == "" {
		return "", fmt.Errorf("query is required for search action")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 30
	}

	switch args.Action {
	case "issues":
		commandArgs := []string{"search", "issues", args.Query,
			"--json", "number,title,state,repository",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "pulls":
		commandArgs := []string{"search", "prs", args.Query,
			"--json", "number,title,state,repository",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "code":
		commandArgs := []string{"search", "code", args.Query,
			"--json", "path,repository,textMatches",
			"--limit", strconv.Itoa(limit)}
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown search action: %s", args.Action)
	}
}
