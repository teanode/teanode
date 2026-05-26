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
	var arguments struct {
		Action       string `json:"action"`
		Channel      string `json:"channel"`
		Limit        int    `json:"limit"`
		IncludeAll   bool   `json:"include_all"`
		Private      bool   `json:"private"`
		DisplayName  string `json:"display_name"`
		MentionsOnly bool   `json:"mentions_only"`
		Team         string `json:"team"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("mattermost: parsing arguments: %w", err)
	}

	exec := func(arguments2 ...string) (string, error) {
		return execMattermostWithTeam(ctx, self.runner, self.binary, arguments.Team, arguments2...)
	}

	switch arguments.Action {
	case "list":
		if arguments.IncludeAll {
			return exec("channel", "list", "--all")
		}
		return exec("channel", "list")

	case "info":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for info action")
		}
		return exec("channel", "info", arguments.Channel)

	case "members":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for members action")
		}
		return exec("channel", "members", arguments.Channel)

	case "unread":
		if arguments.MentionsOnly {
			return exec("channel", "unread", "--mentions")
		}
		return exec("channel", "unread")

	case "messages":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for messages action")
		}
		limit := arguments.Limit
		if limit <= 0 {
			limit = 20
		}
		return exec("post", "list", arguments.Channel, "-n", fmt.Sprintf("%d", limit))

	case "unread_messages":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for unread_messages action")
		}
		return exec("post", "unread", arguments.Channel)

	case "join":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for join action")
		}
		output, err := exec("channel", "join", arguments.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("joined", output), nil

	case "leave":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for leave action")
		}
		output, err := exec("channel", "leave", arguments.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("left", output), nil

	case "create":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for create action")
		}
		commandArguments := []string{"channel", "create", arguments.Channel}
		if arguments.DisplayName != "" {
			commandArguments = append(commandArguments, "--display-name", arguments.DisplayName)
		}
		if arguments.Private {
			commandArguments = append(commandArguments, "--private")
		}
		return exec(commandArguments...)

	case "archive":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for archive action")
		}
		output, err := exec("channel", "archive", arguments.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("archived", output), nil

	case "mark_read":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for mark_read action")
		}
		output, err := exec("channel", "read", arguments.Channel)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("marked_read", output), nil

	default:
		return "", fmt.Errorf("mattermost: unknown channels action: %s", arguments.Action)
	}
}
