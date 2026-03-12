package gitea

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
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

func (self *pullsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
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
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		limit := args.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArgs := []string{"pulls", "list",
			"--output", "json",
			"--limit", strconv.Itoa(limit)}
		if args.State != "" {
			commandArgs = append(commandArgs, "--state", args.State)
		}
		appendRepository(&commandArgs, args.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArgs...)

	case "view":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for view action")
		}
		commandArgs := []string{"pulls", strconv.Itoa(args.Number),
			"--output", "json", "--comments"}
		appendRepository(&commandArgs, args.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		if args.Description == "" {
			return "", fmt.Errorf("description is required for create action")
		}
		commandArgs := []string{"pulls", "create",
			"--title", args.Title, "--description", args.Description}
		if args.Head != "" {
			commandArgs = append(commandArgs, "--head", args.Head)
		}
		if args.Base != "" {
			commandArgs = append(commandArgs, "--base", args.Base)
		}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--labels", args.Labels)
		}
		if args.Assignees != "" {
			commandArgs = append(commandArgs, "--assignees", args.Assignees)
		}
		if args.Milestone != "" {
			commandArgs = append(commandArgs, "--milestone", args.Milestone)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "comment":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for comment action")
		}
		if args.Description == "" {
			return "", fmt.Errorf("description is required for comment action")
		}
		commandArgs := []string{"comment", strconv.Itoa(args.Number), args.Description}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "close":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for close action")
		}
		commandArgs := []string{"pulls", "close", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("closed", output), nil

	case "reopen":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for reopen action")
		}
		commandArgs := []string{"pulls", "reopen", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "merge":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for merge action")
		}
		commandArgs := []string{"pulls", "merge", strconv.Itoa(args.Number)}
		if args.MergeStyle != "" {
			commandArgs = append(commandArgs, "--style", args.MergeStyle)
		}
		if args.MergeMessage != "" {
			commandArgs = append(commandArgs, "--message", args.MergeMessage)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("merged", output), nil

	case "approve":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for approve action")
		}
		commandArgs := []string{"pulls", "approve", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("approved", output), nil

	case "reject":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for reject action")
		}
		commandArgs := []string{"pulls", "reject", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("rejected", output), nil

	default:
		return "", fmt.Errorf("unknown pulls action: %s", args.Action)
	}
}
