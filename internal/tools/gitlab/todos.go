package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type todosTool struct {
	binary string
	runner commandRunner
}

func (self *todosTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "gitlab_todos",
			Description: "Interact with GitLab to-do items. Actions: list (list current user's todos), " +
				"mark_done (mark a todo item as done), mark_all_done (mark all pending todos as done).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "mark_done", "mark_all_done"},
						"description": "The todo action to perform.",
					},
					"id": map[string]interface{}{
						"type":        "integer",
						"description": "Todo item ID (for mark_done action).",
					},
					"state": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"pending", "done"},
						"description": "Filter by todo state for list action. Defaults to pending, matching GitLab's API behavior.",
					},
					"action_type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"assigned", "mentioned", "build_failed", "marked", "approval_required", "unmergeable", "directly_addressed", "merge_train_removed", "member_access_requested"},
						"description": "Filter by todo action type for list action.",
					},
					"author_id": map[string]interface{}{
						"type":        "integer",
						"description": "Filter by author ID for list action.",
					},
					"project_id": map[string]interface{}{
						"type":        "integer",
						"description": "Filter by project ID for list action.",
					},
					"group_id": map[string]interface{}{
						"type":        "integer",
						"description": "Filter by group ID for list action.",
					},
					"target_type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"Issue", "MergeRequest", "Commit", "Epic", "DesignManagement::Design", "AlertManagement::Alert", "Project", "Namespace", "Vulnerability", "WikiPage::Meta"},
						"description": "Filter by todo target type for list action.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "JSON object with to-do data from GitLab.",
			},
		},
	}
}

func (self *todosTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *todosTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		ID         int    `json:"id"`
		State      string `json:"state"`
		ActionType string `json:"action_type"`
		AuthorID   int    `json:"author_id"`
		ProjectID  int    `json:"project_id"`
		GroupID    int    `json:"group_id"`
		TargetType string `json:"target_type"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		commandArgs := []string{"api", "todos", "--method", "GET", "--paginate"}
		if args.State != "" {
			commandArgs = append(commandArgs, "-F", "state="+args.State)
		}
		if args.ActionType != "" {
			commandArgs = append(commandArgs, "-F", "action="+args.ActionType)
		}
		if args.AuthorID > 0 {
			commandArgs = append(commandArgs, "-F", "author_id="+strconv.Itoa(args.AuthorID))
		}
		if args.ProjectID > 0 {
			commandArgs = append(commandArgs, "-F", "project_id="+strconv.Itoa(args.ProjectID))
		}
		if args.GroupID > 0 {
			commandArgs = append(commandArgs, "-F", "group_id="+strconv.Itoa(args.GroupID))
		}
		if args.TargetType != "" {
			commandArgs = append(commandArgs, "-F", "type="+args.TargetType)
		}
		return execGitLab(ctx, self.runner, self.binary, commandArgs...)

	case "mark_done":
		if args.ID == 0 {
			return "", fmt.Errorf("id is required for mark_done action")
		}
		output, err := execGitLab(ctx, self.runner, self.binary, "api", "todos/"+strconv.Itoa(args.ID)+"/mark_as_done", "--method", "POST")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("marked_done", output), nil

	case "mark_all_done":
		output, err := execGitLab(ctx, self.runner, self.binary, "api", "todos/mark_as_done", "--method", "POST")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("marked_all_done", output), nil

	default:
		return "", fmt.Errorf("unknown todos action: %s", args.Action)
	}
}
