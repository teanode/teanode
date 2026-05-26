package google

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type tasksTool struct {
	binary string

	runner commandRunner
}

func (self *tasksTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "google_tasks",
			Description: "Interact with Google Tasks. Actions: list (show tasks), create (new task), " +
				"complete (mark done), delete (remove task). All actions require a task_list ID.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "create", "complete", "delete"},
						"description": "The Tasks action to perform.",
					},
					"task_list": map[string]interface{}{
						"type":        "string",
						"description": "Task list ID (required for all actions).",
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
						"description": "Due date in ISO 8601 or YYYY-MM-DD (for 'create' action, optional).",
					},
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "Task ID (for 'complete' and 'delete' actions).",
					},
				},
				"required": []string{"action", "task_list"},
			},
		},
	}
}

func (self *tasksTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *tasksTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action   string `json:"action"`
		TaskList string `json:"task_list"`
		Title    string `json:"title"`
		Notes    string `json:"notes"`
		Due      string `json:"due"`
		TaskID   string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("google: parsing arguments: %w", err)
	}

	if arguments.TaskList == "" {
		return "", fmt.Errorf("google: task_list is required for all tasks actions")
	}

	switch arguments.Action {
	case "list":
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"tasks", "list", arguments.TaskList)

	case "create":
		if arguments.Title == "" {
			return "", fmt.Errorf("google: title is required for create action")
		}
		commandArguments := []string{"tasks", "create", arguments.TaskList, "--title", arguments.Title}
		if arguments.Notes != "" {
			commandArguments = append(commandArguments, "--notes", arguments.Notes)
		}
		if arguments.Due != "" {
			commandArguments = append(commandArguments, "--due", arguments.Due)
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account, commandArguments...)

	case "complete":
		if arguments.TaskID == "" {
			return "", fmt.Errorf("google: task_id is required for complete action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"tasks", "complete", arguments.TaskList, arguments.TaskID)

	case "delete":
		if arguments.TaskID == "" {
			return "", fmt.Errorf("google: task_id is required for delete action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"tasks", "delete", arguments.TaskList, arguments.TaskID)

	default:
		return "", fmt.Errorf("google: unknown tasks action: %s", arguments.Action)
	}
}
