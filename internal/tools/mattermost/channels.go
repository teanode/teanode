package mattermost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type channelsTool struct {
	binary string
	runner commandRunner
}

func (self *channelsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "mattermost_channels",
			Description: "Interact with Mattermost channels. Actions: list (list channels), " +
				"info (get channel details), members (list channel members), unread (list channels with unread messages), " +
				"messages (read recent messages), unread_messages (read unread messages), " +
				"join (join a channel), leave (leave a channel), create (create a channel), " +
				"archive (archive a channel), mark_read (mark a channel as read).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "info", "members", "unread", "messages", "unread_messages", "join", "leave", "create", "archive", "mark_read"},
						"description": "The channel action to perform.",
					},
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Channel name or ID (required for most actions except 'list' and 'unread').",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of messages to return (for 'messages' action, default 20).",
					},
					"include_all": map[string]interface{}{
						"type":        "boolean",
						"description": "Include channels you have not joined yet (for 'list' action).",
					},
					"private": map[string]interface{}{
						"type":        "boolean",
						"description": "Create as private channel (for 'create' action).",
					},
					"display_name": map[string]interface{}{
						"type":        "string",
						"description": "Display name for the channel (for 'create' action, defaults to channel name).",
					},
					"mentions_only": map[string]interface{}{
						"type":        "boolean",
						"description": "Only show channels with mentions (for 'unread' action).",
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

func (self *channelsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "info", "members", "unread", "messages", "unread_messages"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *channelsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action       string `json:"action"`
		Channel      string `json:"channel"`
		Limit        int    `json:"limit"`
		IncludeAll   bool   `json:"include_all"`
		Private      bool   `json:"private"`
		DisplayName  string `json:"display_name"`
		MentionsOnly bool   `json:"mentions_only"`
		Team         string `json:"team"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	exec := func(args2 ...string) (string, error) {
		return execMattermostWithTeam(ctx, self.runner, self.binary, args.Team, args2...)
	}

	switch args.Action {
	case "list":
		if args.IncludeAll {
			return exec("channel", "list", "--all")
		}
		return exec("channel", "list")

	case "info":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for info action")
		}
		return exec("channel", "info", args.Channel)

	case "members":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for members action")
		}
		return exec("channel", "members", args.Channel)

	case "unread":
		if args.MentionsOnly {
			return exec("channel", "unread", "--mentions")
		}
		return exec("channel", "unread")

	case "messages":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for messages action")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}
		return exec("post", "list", args.Channel, "-n", fmt.Sprintf("%d", limit))

	case "unread_messages":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for unread_messages action")
		}
		return exec("post", "unread", args.Channel)

	case "join":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for join action")
		}
		output, err := exec("channel", "join", args.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("joined", output), nil

	case "leave":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for leave action")
		}
		output, err := exec("channel", "leave", args.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("left", output), nil

	case "create":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for create action")
		}
		commandArgs := []string{"channel", "create", args.Channel}
		if args.DisplayName != "" {
			commandArgs = append(commandArgs, "--display-name", args.DisplayName)
		}
		if args.Private {
			commandArgs = append(commandArgs, "--private")
		}
		return exec(commandArgs...)

	case "archive":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for archive action")
		}
		output, err := exec("channel", "archive", args.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("archived", output), nil

	case "mark_read":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for mark_read action")
		}
		output, err := exec("channel", "read", args.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("marked_read", output), nil

	default:
		return "", fmt.Errorf("unknown channels action: %s", args.Action)
	}
}
