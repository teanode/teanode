package github

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
			Name: "github_pulls",
			Description: "Interact with GitHub pull requests. Actions: list (list PRs), view (get PR details), " +
				"create (open new PR), comment (add comment), merge (merge PR), diff (get PR diff), " +
				"checks (view CI status).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "create", "comment", "merge", "diff", "checks"},
						"description": "The pull request action to perform.",
					},
					"number": map[string]interface{}{
						"type":        "integer",
						"description": "Pull request number (for view, comment, merge, diff, checks actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Pull request title (for create action).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Pull request body or comment text (for create and comment actions).",
					},
					"head": map[string]interface{}{
						"type":        "string",
						"description": "Head branch name (for create action).",
					},
					"base": map[string]interface{}{
						"type":        "string",
						"description": "Base branch name (for create action, defaults to repo default branch).",
					},
					"merge_method": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"merge", "squash", "rebase"},
						"description": "Merge method (for merge action, default merge).",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Filter by assignee username, use \"@me\" to refer to current user (for list action).",
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Filter by author username, use \"@me\" to refer to current user (for list action).",
					},
					"state": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"open", "closed", "merged", "all"},
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
				"description": "JSON object with pull request data from GitHub.",
			},
		},
	}
}

func (self *pullsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		Head        string `json:"head"`
		Base        string `json:"base"`
		MergeMethod string `json:"merge_method"`
		Assignee    string `json:"assignee"`
		Author      string `json:"author"`
		State       string `json:"state"`
		Limit       int    `json:"limit"`
		Repository  string `json:"repository"`
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
		commandArgs := []string{"pr", "list",
			"--json", "number,title,state,author,headRefName,baseRefName,isDraft,createdAt",
			"--limit", strconv.Itoa(limit)}
		if args.State != "" {
			commandArgs = append(commandArgs, "--state", args.State)
		}
		if args.Assignee != "" {
			commandArgs = append(commandArgs, "--assignee", args.Assignee)
		}
		if args.Author != "" {
			commandArgs = append(commandArgs, "--author", args.Author)
		}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "view":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for view action")
		}
		commandArgs := []string{"pr", "view", strconv.Itoa(args.Number),
			"--json", "number,title,state,body,author,headRefName,baseRefName,reviews,comments,mergeable,additions,deletions"}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		if args.Body == "" {
			return "", fmt.Errorf("body is required for create action")
		}
		if args.Head == "" {
			return "", fmt.Errorf("head is required for create action")
		}
		commandArgs := []string{"pr", "create", "--title", args.Title, "--body", args.Body, "--head", args.Head}
		if args.Base != "" {
			commandArgs = append(commandArgs, "--base", args.Base)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "comment":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for comment action")
		}
		if args.Body == "" {
			return "", fmt.Errorf("body is required for comment action")
		}
		commandArgs := []string{"pr", "comment", strconv.Itoa(args.Number), "--body", args.Body}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "merge":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for merge action")
		}
		mergeMethod := args.MergeMethod
		if mergeMethod == "" {
			mergeMethod = "merge"
		}
		commandArgs := []string{"pr", "merge", strconv.Itoa(args.Number), "--" + mergeMethod}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("merged", output), nil

	case "diff":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for diff action")
		}
		commandArgs := []string{"pr", "diff", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		envelope := map[string]string{"diff": output}
		data, _ := json.Marshal(envelope)
		return string(data), nil

	case "checks":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for checks action")
		}
		commandArgs := []string{"pr", "checks", strconv.Itoa(args.Number),
			"--json", "name,state,description"}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	default:
		return "", fmt.Errorf("unknown pulls action: %s", args.Action)
	}
}
