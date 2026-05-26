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

type issuesTool struct {
	binary string
	runner commandRunner
}

func (self *issuesTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "github_issues",
			Description: "Interact with GitHub issues. Actions: list (list issues), view (get issue details), " +
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
						"description": "Issue number (for view, comment, close, reopen, edit actions).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Issue title (for create and edit actions).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Issue body or comment text (for create, comment, edit actions).",
					},
					"labels": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated labels (for create and edit actions).",
					},
					"assignee": map[string]interface{}{
						"type":        "string",
						"description": "Filter by assignee username, use \"@me\" to refer to current user (for list action).",
					},
					"author": map[string]interface{}{
						"type":        "string",
						"description": "Filter by author username, use \"@me\" to refer to current user (for list action).",
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
						"description": "Search issues using GitHub issue search syntax (for list action).",
					},
					"mention": map[string]interface{}{
						"type":        "string",
						"description": "Filter by mentioned username (for list action).",
					},
					"milestone": map[string]interface{}{
						"type":        "string",
						"description": "Filter by milestone number or title (for list action).",
					},
					"app": map[string]interface{}{
						"type":        "string",
						"description": "Filter by GitHub App author (for list action).",
					},
					"assignees": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated assignee usernames (for create action).",
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
				"description": "JSON object with issue data from GitHub.",
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
		Action     string `json:"action"`
		Number     int    `json:"number"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Labels     string `json:"labels"`
		Assignee   string `json:"assignee"`
		Author     string `json:"author"`
		Label      any    `json:"label"`
		Search     string `json:"search"`
		Mention    string `json:"mention"`
		Milestone  string `json:"milestone"`
		App        string `json:"app"`
		Assignees  string `json:"assignees"`
		State      string `json:"state"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
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
		commandArguments := []string{"issue", "list",
			"--json", "number,title,state,author,assignees,labels,createdAt",
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
		commandArguments = appendStringFlags(commandArguments, "--label", arguments.Label)
		search := arguments.Search
		if search == "" {
			search = "sort:updated-desc"
		}
		if search != "" {
			commandArguments = append(commandArguments, "--search", search)
		}
		if arguments.Mention != "" {
			commandArguments = append(commandArguments, "--mention", arguments.Mention)
		}
		if arguments.Milestone != "" {
			commandArguments = append(commandArguments, "--milestone", arguments.Milestone)
		}
		if arguments.App != "" {
			commandArguments = append(commandArguments, "--app", arguments.App)
		}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "view":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for view action")
		}
		commandArguments := []string{"issue", "view", strconv.Itoa(arguments.Number),
			"--json", "number,title,state,body,author,assignees,labels,comments,createdAt"}
		appendRepository(&commandArguments, arguments.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("github: title is required for create action")
		}
		if arguments.Body == "" {
			return "", fmt.Errorf("github: body is required for create action")
		}
		commandArguments := []string{"issue", "create", "--title", arguments.Title, "--body", arguments.Body}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--label", arguments.Labels)
		}
		if arguments.Assignees != "" {
			commandArguments = append(commandArguments, "--assignee", arguments.Assignees)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("created", output), nil

	case "comment":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for comment action")
		}
		if arguments.Body == "" {
			return "", fmt.Errorf("github: body is required for comment action")
		}
		commandArguments := []string{"issue", "comment", strconv.Itoa(arguments.Number), "--body", arguments.Body}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("commented", output), nil

	case "close":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for close action")
		}
		commandArguments := []string{"issue", "close", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("closed", output), nil

	case "reopen":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for reopen action")
		}
		commandArguments := []string{"issue", "reopen", strconv.Itoa(arguments.Number)}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "edit":
		if arguments.Number == 0 {
			return "", fmt.Errorf("github: number is required for edit action")
		}
		commandArguments := []string{"issue", "edit", strconv.Itoa(arguments.Number)}
		if arguments.Title != "" {
			commandArguments = append(commandArguments, "--title", arguments.Title)
		}
		if arguments.Body != "" {
			commandArguments = append(commandArguments, "--body", arguments.Body)
		}
		if arguments.Labels != "" {
			commandArguments = append(commandArguments, "--add-label", arguments.Labels)
		}
		appendRepository(&commandArguments, arguments.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArguments...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("edited", output), nil

	default:
		return "", fmt.Errorf("github: unknown issues action: %s", arguments.Action)
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
