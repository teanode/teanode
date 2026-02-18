package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
)

type releasesTool struct {
	binary string
	runner commandRunner
}

func (self *releasesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "github_releases",
			Description: "Interact with GitHub releases. Actions: list (list releases), " +
				"create (create a new release).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "create"},
						"description": "The releases action to perform.",
					},
					"tag": map[string]interface{}{
						"type":        "string",
						"description": "Git tag for the release (for create action).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Release title (for create action).",
					},
					"notes": map[string]interface{}{
						"type":        "string",
						"description": "Release notes (for create action).",
					},
					"draft": map[string]interface{}{
						"type":        "boolean",
						"description": "Create as draft release (for create action).",
					},
					"prerelease": map[string]interface{}{
						"type":        "boolean",
						"description": "Mark as pre-release (for create action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for list action, default 30).",
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
				"description": "JSON object with release data from GitHub.",
			},
		},
	}
}

func (self *releasesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Tag        string `json:"tag"`
		Title      string `json:"title"`
		Notes      string `json:"notes"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		limit := args.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArgs := []string{"release", "list",
			"--json", "tagName,name,isDraft,isPrerelease,publishedAt",
			"--limit", strconv.Itoa(limit)}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Tag == "" {
			return "", fmt.Errorf("tag is required for create action")
		}
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		commandArgs := []string{"release", "create", args.Tag, "--title", args.Title}
		if args.Notes != "" {
			commandArgs = append(commandArgs, "--notes", args.Notes)
		}
		if args.Draft {
			commandArgs = append(commandArgs, "--draft")
		}
		if args.Prerelease {
			commandArgs = append(commandArgs, "--prerelease")
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	default:
		return "", fmt.Errorf("unknown releases action: %s", args.Action)
	}
}
