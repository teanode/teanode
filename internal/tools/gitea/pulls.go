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

type pullsTool struct {
	binary string
	runner commandRunner
}

func (self *pullsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitea_pulls",
			Description: "Interact with Gitea pull requests. Actions: list (list PRs), view (get PR details), " +
				"create (open new PR), comment (add comment), close (close PR), reopen (reopen PR), " +
				"merge (merge PR), approve (approve PR), reject (request changes).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "create", "comment", "close", "reopen", "merge", "approve", "reject"},
						"description": "The pull request action to perform.",
					},
					"number": map[string]interface{}{
						"type":        "integer",
						"description": "Pull request index (for view, comment, close, reopen, merge, approve, reject actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Pull request title (for create action).",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Pull request description or comment text (for create and comment actions).",
					},
					"head": map[string]interface{}{
						"type":        "string",
						"description": "Source branch name. To specify a different head repo, use <user>:<branch> (for create action, defaults to current branch).",
					},
					"base": map[string]interface{}{
						"type":        "string",
						"description": "Target branch name (for create action, defaults to repo default branch).",
					},
					"merge_style": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"merge", "rebase", "squash", "rebase-merge"},
						"description": "Merge style (for merge action, default merge).",
					},
					"merge_message": map[string]interface{}{
						"type":        "string",
						"description": "Merge commit message (for merge action).",
					},
					"labels": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated labels to assign (for create action).",
					},
					"assignees": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated list of usernames to assign (for create action).",
					},
					"milestone": map[string]interface{}{
						"type":        "string",
						"description": "Milestone to assign (for create action).",
					},
					"state": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "closed", "all"},
						"description": "Filter by state (for list action, default open).",
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
				"description": "JSON object with pull request data from Gitea.",
			},
		},
	}
}

func (self *pullsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *pullsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action       string `json:"action"`
		Number       int    `json:"number"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		Head         string `json:"head"`
		Base         string `json:"base"`
		MergeStyle   string `json:"merge_style"`
		MergeMessage string `json:"merge_message"`
		Labels       string `json:"labels"`
		Assignees    string `json:"assignees"`
		Milestone    string `json:"milestone"`
		State        string `json:"state"`
		Limit        int    `json:"limit"`
		Repository   string `json:"repository"`
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
		commandArguments := []string{"pulls", "list",
			"--output", "json",
			"--limit", strconv.Itoa(limit)}
		if arguments.State != "" {
			commandArguments = append(commandArguments, "--state", arguments.State)
		}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for view action")
		}
		commandArguments := []string{"pulls", strconv.Itoa(arguments.Number),
			"--output", "json", "--comments"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("gitea: title is required for create action")
		}
		if arguments.Description == "" {
			return "", fmt.Errorf("gitea: description is required for create action")
		}
		commandArguments := []string{"pulls", "create",
			"--title", arguments.Title, "--description", arguments.Description}
		if arguments.Head != "" {
			commandArguments = append(commandArguments, "--head", arguments.Head)
		}
		if arguments.Base != "" {
			commandArguments = append(commandArguments, "--base", arguments.Base)
		}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--labels", arguments.Labels)
		}
		if arguments.Assignees != "" {
			commandArguments = append(commandArguments, "--assignees", arguments.Assignees)
		}
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestone", arguments.Milestone)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "comment":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for comment action")
		}
		if arguments.Description == "" {
			return "", fmt.Errorf("gitea: description is required for comment action")
		}
		commandArguments := []string{"comment", strconv.Itoa(arguments.Number), arguments.Description}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "close":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for close action")
		}
		commandArguments := []string{"pulls", "close", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("closed", output), nil

	case "reopen":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for reopen action")
		}
		commandArguments := []string{"pulls", "reopen", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "merge":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for merge action")
		}
		commandArguments := []string{"pulls", "merge", strconv.Itoa(arguments.Number)}
		if arguments.MergeStyle != "" {
			commandArguments = append(commandArguments, "--style", arguments.MergeStyle)
		}
		if arguments.MergeMessage != "" {
			commandArguments = append(commandArguments, "--message", arguments.MergeMessage)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("merged", output), nil

	case "approve":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for approve action")
		}
		commandArguments := []string{"pulls", "approve", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("approved", output), nil

	case "reject":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for reject action")
		}
		commandArguments := []string{"pulls", "reject", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("rejected", output), nil

	default:
		return "", fmt.Errorf("gitea: unknown pulls action: %s", arguments.Action)
	}
}
