package gitlab

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

func (self *issuesTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *issuesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
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
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("gitlab: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		perPage := arguments.PerPage
		if perPage <= 0 {
			perPage = 30
		}
		commandArguments := []string{"issue", "list",
			"--output", "json",
			"--order", "updated_at",
			"--sort", "desc",
			"--per-page", strconv.Itoa(perPage)}
		switch arguments.State {
		case "closed":
			commandArguments = append(commandArguments, "--closed")
		case "all":
			commandArguments = append(commandArguments, "--all")
		case "opened", "":
			// Default behavior: glab lists opened issues without any flag.
		}
		if arguments.Assignee != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignee)
		}
		if arguments.Author != "" {
			commandArguments = append(commandArguments, "--author", arguments.Author)
		}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for view action")
		}
		commandArguments := []string{"issue", "view", strconv.Itoa(arguments.Number),
			"--output", "json", "--comments"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitLab(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("gitlab: title is required for create action")
		}
		if arguments.Description == "" {
			return "", fmt.Errorf("gitlab: description is required for create action")
		}
		commandArguments := []string{"issue", "create",
			"--title", arguments.Title, "--description", arguments.Description}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--label", arguments.Labels)
		}
		if arguments.Assignees != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignees)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "comment":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for comment action")
		}
		if arguments.Description == "" {
			return "", fmt.Errorf("gitlab: description is required for comment action")
		}
		commandArguments := []string{"issue", "note", strconv.Itoa(arguments.Number),
			"--message", arguments.Description}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "close":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for close action")
		}
		commandArguments := []string{"issue", "close", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("closed", output), nil

	case "reopen":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for reopen action")
		}
		commandArguments := []string{"issue", "reopen", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "edit":
		if arguments.Number == 0 {
			return "", fmt.Errorf("gitlab: number is required for edit action")
		}
		hasUpdate := arguments.Title != "" || arguments.Description != "" || arguments.Labels != "" ||
			arguments.Unlabel != "" || arguments.Assignees != "" || arguments.Unassign ||
			arguments.Milestone != "" || arguments.DueDate != "" || arguments.Confidential != nil
		if !hasUpdate {
			return "", fmt.Errorf("gitlab: at least one field to update is required (title, description, labels, unlabel, assignees, unassign, milestone, due_date, confidential)")
		}
		commandArguments := []string{"issue", "update", strconv.Itoa(arguments.Number)}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, "--title", arguments.Title)
		}
		if arguments.Description != "" {
			commandArguments = append(commandArguments, "--description", arguments.Description)
		}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--label", arguments.Labels)
		}
		if arguments.Unlabel != "" {
			commandArguments = append(commandArguments, "--unlabel", arguments.Unlabel)
		}
		if arguments.Assignees != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignees)
		}
		if arguments.Unassign {
			commandArguments = append(commandArguments, "--unassign")
		}
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestone", arguments.Milestone)
		}
		if arguments.DueDate != "" {
			commandArguments = append(commandArguments, "--due-date", arguments.DueDate)
		}
		if arguments.Confidential != nil {
			if *arguments.Confidential {
				commandArguments = append(commandArguments, "--confidential")
			} else {
				commandArguments = append(commandArguments, "--public")
			}
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitLab(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("edited", output), nil

	default:
		return "", fmt.Errorf("gitlab: unknown issues action: %s", arguments.Action)
	}
}

// appendRepository adds the -R flag if repository is non-empty.
func appendRepository(commandArguments *[]string, repository string) {
	if repository != "" {
		*commandArguments = append(*commandArguments, "-R", repository)
	}
}

// appendStringFlags appends repeated string flags from either a string or []string input.
func appendStringFlags(commandArguments []string, flag string, value any) []string {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			commandArguments = append(commandArguments, flag, typed)
		}
	case []any:
		for _, raw := range typed {
			text, ok := raw.(string)
			if ok && text != "" {
				commandArguments = append(commandArguments, flag, text)
			}
		}
	case []string:
		for _, text := range typed {
			if text != "" {
				commandArguments = append(commandArguments, flag, text)
			}
		}
	}
	return commandArguments
}

// wrapPlainOutput wraps non-JSON command output in a JSON envelope.
func wrapPlainOutput(status string, output string) string {
	envelope := map[string]string{"status": status, "message": output}
	data, _ := json.Marshal(envelope)
	return string(data)
}
