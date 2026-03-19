package mattermost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type threadsTool struct {
	binary string
	runner commandRunner
}

func (self *threadsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "mattermost_threads",
			Description: "Manage Mattermost threads. Actions: list (list your threads), " +
				"view (view a thread), follow/unfollow (manage thread following), " +
				"mark_read (mark a thread as read), mark_unread (mark a thread as unread), " +
				"read_all (mark all threads as read).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "view", "follow", "unfollow", "mark_read", "mark_unread", "read_all"},
						"description": "The thread action to perform.",
					},
					"thread_id": map[string]interface{}{
						"type":        "string",
						"description": "Thread/post ID (for 'view', 'follow', 'unfollow', 'mark_read', 'mark_unread' actions).",
					},
					"unread_only": map[string]interface{}{
						"type":        "boolean",
						"description": "Only show unread threads (for 'list' action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of threads to return (for 'list' action, default 20).",
					},
					"team": map[string]interface{}{
						"type":        "string",
						"description": "Team name to operate on. If omitted, uses the active team.",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *threadsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "view"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *threadsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		ThreadID   string `json:"thread_id"`
		UnreadOnly bool   `json:"unread_only"`
		Limit      int    `json:"limit"`
		Team       string `json:"team"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	exec := func(args2 ...string) (string, error) {
		return execMattermostWithTeam(ctx, self.runner, self.binary, args.Team, args2...)
	}

	switch args.Action {
	case "list":
		commandArgs := []string{"thread", "list"}
		if args.UnreadOnly {
			commandArgs = append(commandArgs, "--unread")
		}
		limit := args.Limit
		if limit > 0 {
			commandArgs = append(commandArgs, "-n", fmt.Sprintf("%d", limit))
		}
		return exec(commandArgs...)

	case "view":
		if args.ThreadID == "" {
			return "", fmt.Errorf("thread_id is required for view action")
		}
		return exec("thread", "view", args.ThreadID)

	case "follow":
		if args.ThreadID == "" {
			return "", fmt.Errorf("thread_id is required for follow action")
		}
		output, err := exec("thread", "follow", args.ThreadID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("following", output), nil

	case "unfollow":
		if args.ThreadID == "" {
			return "", fmt.Errorf("thread_id is required for unfollow action")
		}
		output, err := exec("thread", "unfollow", args.ThreadID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unfollowed", output), nil

	case "mark_read":
		if args.ThreadID == "" {
			return "", fmt.Errorf("thread_id is required for mark_read action")
		}
		output, err := exec("thread", "read", args.ThreadID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("marked_read", output), nil

	case "mark_unread":
		if args.ThreadID == "" {
			return "", fmt.Errorf("thread_id is required for mark_unread action")
		}
		output, err := exec("thread", "unread", args.ThreadID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("marked_unread", output), nil

	case "read_all":
		output, err := exec("thread", "read-all")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("all_read", output), nil

	default:
		return "", fmt.Errorf("unknown threads action: %s", args.Action)
	}
}
