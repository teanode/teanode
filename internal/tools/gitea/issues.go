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
	var arguments struct {
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
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitea: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		limit := arguments.Limit
		if limit <= 0 {
			limit = 30
		}
		commandArguments := []string{"issues", "list",
			"--output", "json",
			"--limit", strconv.Itoa(limit)}
		if arguments.State != "" {
			commandArguments = append(commandArguments, "--state", arguments.State)
		}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--labels", arguments.Labels)
		}
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestones", arguments.Milestone)
		}
		if arguments.Assignee != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignee)
		}
		if arguments.Author != "" {
			commandArguments = append(commandArguments, "--author", arguments.Author)
		}
		if arguments.Keyword != "" {
			commandArguments = append(commandArguments, "--keyword", arguments.Keyword)
		}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitea(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for view action")
		}
		commandArguments := []string{"issues", strconv.Itoa(arguments.Number),
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
		commandArguments := []string{"issues", "create",
			"--title", arguments.Title, "--description", arguments.Description}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--labels", arguments.Labels)
		}
		if arguments.Assignees != "" {
			commandArguments = append(commandArguments, "--assignees", arguments.Assignees)
		}
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestone", arguments.Milestone)
		}
		if arguments.Deadline != "" {
			commandArguments = append(commandArguments, "--deadline", arguments.Deadline)
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
		commandArguments := []string{"issues", "close", strconv.Itoa(arguments.Number)}
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
		commandArguments := []string{"issues", "reopen", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "edit":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitea: number is required for edit action")
		}
		hasUpdate := arguments.Title != "" || arguments.Description != "" || arguments.Labels != "" ||
			arguments.Assignees != "" || arguments.Milestone != "" || arguments.Deadline != ""
		if !hasUpdate {
			return "", fmt.Errorf("gitea: at least one field to update is required (title, description, labels, assignees, milestone, deadline)")
		}
		commandArguments := []string{"issues", "edit", strconv.Itoa(arguments.Number)}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, "--title", arguments.Title)
		}
		if arguments.Description != "" {
			commandArguments = append(commandArguments, "--description", arguments.Description)
		}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--add-labels", arguments.Labels)
		}
		if arguments.Assignees != "" {
			commandArguments = append(commandArguments, "--add-assignees", arguments.Assignees)
		}
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestone", arguments.Milestone)
		}
		if arguments.Deadline != "" {
			commandArguments = append(commandArguments, "--deadline", arguments.Deadline)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitea(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("edited", output), nil

	default:
		return "", fmt.Errorf("gitea: unknown issues action: %s", arguments.Action)
	}
}

// appendRepository adds the --repo flag if repository is non-empty.
func appendRepository(commandArguments *[]string, repository string) {
	if repository != "" {
		*commandArguments = append(*commandArguments, "--repo", repository)
	}
}

// wrapPlainOutput wraps non-JSON command output in a JSON envelope.
func wrapPlainOutput(status string, output string) string {
	envelope := map[string]string{"status": status, "message": output}
	data, _ := json.Marshal(envelope)
	return string(data)
}
