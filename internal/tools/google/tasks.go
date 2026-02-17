package google

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/provider"
)

type tasksTool struct {
	binary  string
	account string
	runner  commandRunner
}

func (self *tasksTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "google_tasks",
			Description: "Interact with Google Tasks. Actions: list (show tasks), create (new task), " +
				"complete (mark done), delete (remove task).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "create", "complete", "delete"},
						"description": "The Tasks action to perform.",
					},
					"list": map[string]interface{}{
						"type":        "string",
						"description": "Task list name (for 'list' and 'create' actions, optional).",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Task title (for 'create' action).",
					},
					"notes": map[string]interface{}{
						"type":        "string",
						"description": "Task notes/description (for 'create' action, optional).",
					},
					"due": map[string]interface{}{
						"type":        "string",
						"description": "Due date in ISO 8601 or natural language (for 'create' action, optional).",
					},
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Task ID (for 'complete' and 'delete' actions).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *tasksTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action string `json:"action"`
		List   string `json:"list"`
		Title  string `json:"title"`
		Notes  string `json:"notes"`
		Due    string `json:"due"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		cmdArgs := []string{"tasks", "list"}
		if args.List != "" {
			cmdArgs = append(cmdArgs, "--list", args.List)
		}
		return execGog(ctx, self.runner, self.binary, self.account, cmdArgs...)

	case "create":
		if args.Title == "" {
			return "", fmt.Errorf("title is required for create action")
		}
		cmdArgs := []string{"tasks", "create", "--title", args.Title}
		if args.List != "" {
			cmdArgs = append(cmdArgs, "--list", args.List)
		}
		if args.Notes != "" {
			cmdArgs = append(cmdArgs, "--notes", args.Notes)
		}
		if args.Due != "" {
			cmdArgs = append(cmdArgs, "--due", args.Due)
		}
		return execGog(ctx, self.runner, self.binary, self.account, cmdArgs...)

	case "complete":
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required for complete action")
		}
		return execGog(ctx, self.runner, self.binary, self.account,
			"tasks", "complete", args.TaskID)

	case "delete":
		if args.TaskID == "" {
			return "", fmt.Errorf("task_id is required for delete action")
		}
		return execGog(ctx, self.runner, self.binary, self.account,
			"tasks", "delete", args.TaskID)

	default:
		return "", fmt.Errorf("unknown tasks action: %s", args.Action)
	}
}
