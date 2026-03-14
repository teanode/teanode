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

type issuesTool struct {
	binary string
	runner commandRunner
}

func (self *issuesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitea_issues",
			Description: "Interact with Gitea issues. Actions: list (list issues), view (get issue details), " +
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
						"description": "Issue index (for view, comment, close, reopen, edit actions).",
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
						"description": "Comma-separated labels (for create, edit, and list actions).",
					},
					"milestone": map[string]interface{}{
						"type":        "string",
						"description": "Milestone to assign or filter by (for create, edit, and list actions).",
					},
					"assignees": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated list of usernames to assign (for create and edit actions).",
					},
					"deadline": map[string]interface{}{
						"type":        "string",
						"description": "Deadline timestamp (for create and edit actions).",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Filter by assignee username (for list action).",
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Filter by author username (for list action).",
					},
					"keyword": map[string]interface{}{
						"type":        "string",
						"description": "Filter by search string (for list action).",
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
				"description": "JSON object with issue data from Gitea.",
			},
		},
	}
}

func (self *issuesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *issuesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Labels      string `json:"labels"`
		Milestone   string `json:"milestone"`
		Assignees   string `json:"assignees"`
		Deadline    string `json:"deadline"`
		Assignee    string `json:"assignee"`
		Author      string `json:"author"`
		Keyword     string `json:"keyword"`
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
		commandArgs := []string{"issues", "list",
			"--output", "json",
			"--limit", strconv.Itoa(limit)}
		if args.State != "" {
			commandArgs = append(commandArgs, "--state", args.State)
		}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--labels", args.Labels)
		}
		if args.Milestone != "" {
			commandArgs = append(commandArgs, "--milestones", args.Milestone)
		}
		if args.Assignee != "" {
			commandArgs = append(commandArgs, "--assignee", args.Assignee)
		}
		if args.Author != "" {
			commandArgs = append(commandArgs, "--author", args.Author)
		}
		if args.Keyword != "" {
			commandArgs = append(commandArgs, "--keyword", args.Keyword)
		}
		appendRepository(&commandArgs, args.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArgs...)

	case "view":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for view action")
		}
		commandArgs := []string{"issues", strconv.Itoa(args.Number),
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
		commandArgs := []string{"issues", "create",
			"--title", args.Title, "--description", args.Description}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--labels", args.Labels)
		}
		if args.Assignees != "" {
			commandArgs = append(commandArgs, "--assignees", args.Assignees)
		}
		if args.Milestone != "" {
			commandArgs = append(commandArgs, "--milestone", args.Milestone)
		}
		if args.Deadline != "" {
			commandArgs = append(commandArgs, "--deadline", args.Deadline)
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
		commandArgs := []string{"issues", "close", strconv.Itoa(args.Number)}
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
		commandArgs := []string{"issues", "reopen", strconv.Itoa(args.Number)}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "edit":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for edit action")
		}
		hasUpdate := args.Title != "" || args.Description != "" || args.Labels != "" ||
			args.Assignees != "" || args.Milestone != "" || args.Deadline != ""
		if !hasUpdate {
			return "", fmt.Errorf("at least one field to update is required (title, description, labels, assignees, milestone, deadline)")
		}
		commandArgs := []string{"issues", "edit", strconv.Itoa(args.Number)}
		if args.Title != "" {
			commandArgs = append(commandArgs, "--title", args.Title)
		}
		if args.Description != "" {
			commandArgs = append(commandArgs, "--description", args.Description)
		}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--add-labels", args.Labels)
		}
		if args.Assignees != "" {
			commandArgs = append(commandArgs, "--add-assignees", args.Assignees)
		}
		if args.Milestone != "" {
			commandArgs = append(commandArgs, "--milestone", args.Milestone)
		}
		if args.Deadline != "" {
			commandArgs = append(commandArgs, "--deadline", args.Deadline)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("edited", output), nil

	default:
		return "", fmt.Errorf("unknown issues action: %s", args.Action)
	}
}

// appendRepository adds the --repo flag if repository is non-empty.
func appendRepository(commandArgs *[]string, repository string) {
	if repository != "" {
		*commandArgs = append(*commandArgs, "--repo", repository)
	}
}

// wrapPlainOutput wraps non-JSON command output in a JSON envelope.
func wrapPlainOutput(status string, output string) string {
	envelope := map[string]string{"status": status, "message": output}
	data, _ := json.Marshal(envelope)
	return string(data)
}
