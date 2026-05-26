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

type releasesTool struct {
	binary string
	runner commandRunner
}

func (self *releasesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitea_releases",
			Description: "Interact with Gitea releases. Actions: list (list releases), " +
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
						"description": "Git tag for the release (for create action). If the tag does not exist yet, it will be created.",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Release title (for create action).",
					},
					"note": map[string]interface{}{
						"type":        "string",
						"description": "Release notes (for create action).",
					},
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Target branch name or commit hash (for create action, defaults to repo default branch).",
					},
					"draft": map[string]interface{}{
						"type":        "boolean",
						"description": "Mark as draft release (for create action).",
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
				"description": "JSON object with release data from Gitea.",
			},
		},
	}
}

func (self *releasesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *releasesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action     string `json:"action"`
		Tag        string `json:"tag"`
		Title      string `json:"title"`
		Note       string `json:"note"`
		Target     string `json:"target"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitea: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		limit := arguments.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArguments := []string{"releases", "list",
			"--output", "json",
			"--limit", strconv.Itoa(limit)}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Tag == "" {
			return "", fmt.Errorf("gitea: tag is required for create action")
		}
		if arguments.Title == "" {
			return "", fmt.Errorf("gitea: title is required for create action")
		}
		commandArguments := []string{"releases", "create",
			"--tag", arguments.Tag, "--title", arguments.Title}
		if arguments.Note != "" {
			commandArguments = append(commandArguments, "--note", arguments.Note)
		}
		if arguments.Target != "" {
			commandArguments = append(commandArguments, "--target", arguments.Target)
		}
		if arguments.Draft {
			commandArguments = append(commandArguments, "--draft")
		}
		if arguments.Prerelease {
			commandArguments = append(commandArguments, "--prerelease")
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	default:
		return "", fmt.Errorf("gitea: unknown releases action: %s", arguments.Action)
	}
}
