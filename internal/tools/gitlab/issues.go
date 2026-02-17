package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/provider"
)

type issuesTool struct {
	binary string
	runner commandRunner
}

func (self *issuesTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "gitlab_issues",
			Description: "Interact with GitLab issues. Actions: list (list issues), view (get issue details), " +
				"create (open new issue), comment (add comment), close (close issue), reopen (reopen issue), " +
				"edit (modify issue).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "create", "comment", "close", "reopen", "edit"},
						"description": "The issues action to perform.",
					},
					"number": map[string]interface{}{
						"type":        "integer",
						"description": "Issue IID (for view, comment, close, reopen, edit actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Issue title (for create and edit actions).",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Issue description or comment text (for create, comment, edit actions).",
					},
					"labels": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated labels to add (for create and edit actions).",
					},
					"unlabel": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated labels to remove (for edit action).",
					},
					"milestone": map[string]interface{}{
						"type":        "string",
						"description": "Milestone title, set to empty string or 0 to unassign (for edit action).",
					},
					"due_date": map[string]interface{}{
						"type":        "string",
						"description": "Due date in YYYY-MM-DD format (for edit action).",
					},
					"confidential": map[string]interface{}{
						"type":        "boolean",
						"description": "Make issue confidential (for edit action).",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Filter by assignee username, use \"@me\" to refer to current user (for list action).",
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Filter by author username, use \"@me\" to refer to current user (for list action).",
					},
					"assignees": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated assignee usernames. For create: set assignees. For edit: prefix with '!' or '-' to remove specific users, '+' to add, or plain to replace all assignees.",
					},
					"unassign": map[string]interface{}{
						"type":        "boolean",
						"description": "Unassign all users from the issue (for edit action).",
					},
					"state": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"opened", "closed", "all"},
						"description": "Filter by state (for list action, default opened).",
					},
					"per_page": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results per page (for list action, default 30).",
					},
					"repository": map[string]interface{}{
						"type":        "string",
						"description": "Target project in OWNER/REPO or GROUP/NAMESPACE/REPO format. If omitted, uses the current repository context.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with issue data from GitLab.",
			},
		},
	}
}

func (self *issuesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action       string `json:"action"`
		Number       int    `json:"number"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		Labels       string `json:"labels"`
		Unlabel      string `json:"unlabel"`
		Milestone    string `json:"milestone"`
		DueDate      string `json:"due_date"`
		Confidential *bool  `json:"confidential"`
		Assignees    string `json:"assignees"`
		Unassign     bool   `json:"unassign"`
		Assignee     string `json:"assignee"`
		Author       string `json:"author"`
		State        string `json:"state"`
		PerPage      int    `json:"per_page"`
		Repository   string `json:"repository"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		perPage := args.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArgs := []string{"issue", "list",
			"--output", "json",
			"--per-page", strconv.Itoa(perPage)}
		switch args.State {
		case "closed":
			commandArgs = append(commandArgs, "--closed")
		case "all":
			commandArgs = append(commandArgs, "--all")
		case "opened", "":
			// Default behavior: glab lists opened issues without any flag.
		}
		if args.Assignee != "" {
			commandArgs = append(commandArgs, "--assignee", args.Assignee)
		}
		if args.Author != "" {
			commandArgs = append(commandArgs, "--author", args.Author)
		}
		appendRepository(&commandArgs, args.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "view":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for view action")
		}
		commandArgs := []string{"issue", "view", strconv.Itoa(args.Number),
			"--output", "json", "--comments"}
		appendRepository(&commandArgs, args.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		if args.Description == "" {
			return "", fmt.Errorf("description is required for create action")
		}
		commandArgs := []string{"issue", "create",
			"--title", args.Title, "--description", args.Description}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--label", args.Labels)
		}
		if args.Assignees != "" {
			commandArgs = append(commandArgs, "--assignee", args.Assignees)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
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
		commandArgs := []string{"issue", "note", strconv.Itoa(args.Number),
			"--message", args.Description}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "close":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for close action")
		}
		commandArgs := []string{"issue", "close", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("closed", output), nil

	case "reopen":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for reopen action")
		}
		commandArgs := []string{"issue", "reopen", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "edit":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for edit action")
		}
		hasUpdate := args.Title != "" || args.Description != "" || args.Labels != "" ||
			args.Unlabel != "" || args.Assignees != "" || args.Unassign ||
			args.Milestone != "" || args.DueDate != "" || args.Confidential != nil
		if !hasUpdate {
			return "", fmt.Errorf("at least one field to update is required (title, description, labels, unlabel, assignees, unassign, milestone, due_date, confidential)")
		}
		commandArgs := []string{"issue", "update", strconv.Itoa(args.Number)}
		if args.Title != "" {
			commandArgs = append(commandArgs, "--title", args.Title)
		}
		if args.Description != "" {
			commandArgs = append(commandArgs, "--description", args.Description)
		}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--label", args.Labels)
		}
		if args.Unlabel != "" {
			commandArgs = append(commandArgs, "--unlabel", args.Unlabel)
		}
		if args.Assignees != "" {
			commandArgs = append(commandArgs, "--assignee", args.Assignees)
		}
		if args.Unassign {
			commandArgs = append(commandArgs, "--unassign")
		}
		if args.Milestone != "" {
			commandArgs = append(commandArgs, "--milestone", args.Milestone)
		}
		if args.DueDate != "" {
			commandArgs = append(commandArgs, "--due-date", args.DueDate)
		}
		if args.Confidential != nil {
			if *args.Confidential {
				commandArgs = append(commandArgs, "--confidential")
			} else {
				commandArgs = append(commandArgs, "--public")
			}
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("edited", output), nil

	default:
		return "", fmt.Errorf("unknown issues action: %s", args.Action)
	}
}

// appendRepository adds the -R flag if repository is non-empty.
func appendRepository(commandArgs *[]string, repository string) {
	if repository != "" {
		*commandArgs = append(*commandArgs, "-R", repository)
	}
}

// wrapPlainOutput wraps non-JSON command output in a JSON envelope.
func wrapPlainOutput(status string, output string) string {
	envelope := map[string]string{"status": status, "message": output}
	data, _ := json.Marshal(envelope)
	return string(data)
}
