package github

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

func (self *issuesTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Number     int    `json:"number"`
		Title      string `json:"title"`
		Body       string `json:"body"`
		Labels     string `json:"labels"`
		Assignee   string `json:"assignee"`
		Author     string `json:"author"`
		Assignees  string `json:"assignees"`
		State      string `json:"state"`
		Limit      int    `json:"limit"`
		Repository string `json:"repository"`
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
		commandArgs := []string{"issue", "list",
			"--json", "number,title,state,author,assignees,labels,createdAt",
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
		commandArgs := []string{"issue", "view", strconv.Itoa(args.Number),
			"--json", "number,title,state,body,author,assignees,labels,comments,createdAt"}
		appendRepository(&commandArgs, args.Repository)
		return execGitHub(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		if args.Body == "" {
			return "", fmt.Errorf("body is required for create action")
		}
		commandArgs := []string{"issue", "create", "--title", args.Title, "--body", args.Body}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--label", args.Labels)
		}
		if args.Assignees != "" {
			commandArgs = append(commandArgs, "--assignee", args.Assignees)
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
		commandArgs := []string{"issue", "comment", strconv.Itoa(args.Number), "--body", args.Body}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
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
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
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
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reopened", output), nil

	case "edit":
		if args.Number == 0 {
			return "", fmt.Errorf("number is required for edit action")
		}
		commandArgs := []string{"issue", "edit", strconv.Itoa(args.Number)}
		if args.Title != "" {
			commandArgs = append(commandArgs, "--title", args.Title)
		}
		if args.Body != "" {
			commandArgs = append(commandArgs, "--body", args.Body)
		}
		if args.Labels != "" {
			commandArgs = append(commandArgs, "--add-label", args.Labels)
		}
		appendRepository(&commandArgs, args.Repository)
		output, err := execGitHub(ctx, self.runner, self.binary, commandArgs...)
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
