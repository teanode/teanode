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
				"create (open new PR), edit (update title/body/base), comment (add comment), merge (merge PR), " +
				"diff (get PR diff), checks (view CI status).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "create", "edit", "comment", "merge", "diff", "checks"},
						"description": "The pull request action to perform.",
					},
					"number": map[string]interface{}{
						"type":        "integer",
						"description": "Pull request number (for view, edit, comment, merge, diff, checks actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Pull request title (for create and edit actions).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Pull request body (for create and edit actions) or comment text (for comment action).",
					},
					"head": map[string]interface{}{
						"type":        "string",
						"description": "Head branch name (for create action and list action filtering).",
					},
					"base": map[string]interface{}{
						"type":        "string",
						"description": "Base branch name (for create, edit, and list actions, defaults to repo default branch when creating).",
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
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Filter by GitHub App author (for list action).",
					},
					"label": map[string]interface{}{
						"oneOf": []map[string]interface{}{
							{"type": "string"},
							{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
						"description": "Filter by one or more labels for list action.",
					},
					"search": map[string]interface{}{
						"type":        "string",
						"description": "Search pull requests using GitHub issue search syntax (for list action).",
					},
					"draft": map[string]interface{}{
						"type":        "boolean",
						"description": "Filter draft pull requests for list action, or create a draft pull request.",
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

func (self *pullsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view", "diff", "checks"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *pullsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		Head        string `json:"head"`
		Base        string `json:"base"`
		MergeMethod string `json:"merge_method"`
		Assignee    string `json:"assignee"`
		Author      string `json:"author"`
		App         string `json:"app"`
		Label       any    `json:"label"`
		Search      string `json:"search"`
		Draft       bool   `json:"draft"`
		State       string `json:"state"`
		Limit       int    `json:"limit"`
		Repository  string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("github: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		limit := arguments.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArguments := []string{"pr", "list",
			"--json", "number,title,state,author,headRefName,baseRefName,isDraft,createdAt",
			"--limit", strconv.Itoa(limit)}
		if arguments.State != "" {
			commandArguments = append(commandArguments, "--state", arguments.State)
		}
		if arguments.Assignee != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignee)
		}
		if arguments.Author != "" {
			commandArguments = append(commandArguments, "--author", arguments.Author)
		}
		if arguments.Base != "" {
			commandArguments = append(commandArguments, "--base", arguments.Base)
		}
		if arguments.Head != "" {
			commandArguments = append(commandArguments, "--head", arguments.Head)
		}
		if arguments.App != "" {
			commandArguments = append(commandArguments, "--app", arguments.App)
		}
		commandArguments = appendStringFlags(commandArguments, "--label", arguments.Label)
		search := arguments.Search
		if search == "" {
			search = "sort:updated-desc"
		}
		if search != "" {
			commandArguments = append(commandArguments, "--search", search)
		}
		if arguments.Draft {
			commandArguments = append(commandArguments, "--draft")
		}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for view action")
		}
		commandArguments := []string{"pr", "view", strconv.Itoa(arguments.Number),
			"--json", "number,title,state,body,author,headRefName,baseRefName,reviews,comments,mergeable,additions,deletions"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("github: title is required for create action")
		}
		if arguments.Body == "" {
			return "", fmt.Errorf("github: body is required for create action")
		}
		if arguments.Head == "" {
			return "", fmt.Errorf("github: head is required for create action")
		}
		commandArguments := []string{"pr", "create", "--title", arguments.Title, "--body", arguments.Body, "--head", arguments.Head}
		if arguments.Base != "" {
			commandArguments = append(commandArguments, "--base", arguments.Base)
		}
		if arguments.Draft {
			commandArguments = append(commandArguments, "--draft")
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "edit":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for edit action")
		}
		commandArguments := []string{"pr", "edit", strconv.Itoa(arguments.Number)}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, "--title", arguments.Title)
		}
		if arguments.Body != "" {
			commandArguments = append(commandArguments, "--body", arguments.Body)
		}
		if arguments.Base != "" {
			commandArguments = append(commandArguments, "--base", arguments.Base)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("edited", output), nil

	case "comment":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for comment action")
		}
		if arguments.Body == "" {
			return "", fmt.Errorf("github: body is required for comment action")
		}
		commandArguments := []string{"pr", "comment", strconv.Itoa(arguments.Number), "--body", arguments.Body}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "merge":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for merge action")
		}
		mergeMethod := arguments.MergeMethod
		if mergeMethod == "" {
			mergeMethod = "merge"
		}
		commandArguments := []string{"pr", "merge", strconv.Itoa(arguments.Number), "--" + mergeMethod}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("merged", output), nil

	case "diff":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for diff action")
		}
		commandArguments := []string{"pr", "diff", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		envelope := map[string]string{"diff": output}
		data, _ := json.Marshal(envelope)
		return string(data), nil

	case "checks":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for checks action")
		}
		commandArguments := []string{"pr", "checks", strconv.Itoa(arguments.Number),
			"--json", "name,state,description"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	default:
		return "", fmt.Errorf("github: unknown pulls action: %s", arguments.Action)
	}
}
