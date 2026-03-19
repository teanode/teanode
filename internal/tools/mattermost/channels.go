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
			Description: "Interact with Mattermost channels. Actions: list (list channels, optionally including unjoined), " +
				"info (get channel details), members (list channel members), unread (list channels with unread messages), " +
				"messages (read recent messages in a channel), unread_messages (read unread messages in a channel).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "info", "members", "unread", "messages", "unread_messages"},
						"description": "The channel action to perform.",
					},
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Channel name or ID (for 'info', 'members', 'messages', 'unread_messages' actions).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of messages to return (for 'messages' action, default 20).",
					},
					"include_all": map[string]interface{}{
						"type":        "boolean",
						"description": "Include channels you have not joined yet (for 'list' action).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *channelsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone},
	}
}

func (self *channelsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action     string `json:"action"`
		Channel    string `json:"channel"`
		Limit      int    `json:"limit"`
		IncludeAll bool   `json:"include_all"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		if args.IncludeAll {
			return execMattermost(ctx, self.runner, self.binary, "channel", "list", "--all")
		}
		return execMattermost(ctx, self.runner, self.binary, "channel", "list")

	case "info":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for info action")
		}
		return execMattermost(ctx, self.runner, self.binary, "channel", "info", args.Channel)

	case "members":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for members action")
		}
		return execMattermost(ctx, self.runner, self.binary, "channel", "members", args.Channel)

	case "unread":
		return execMattermost(ctx, self.runner, self.binary, "channel", "unread")

	case "messages":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for messages action")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "list", args.Channel,
			"-n", fmt.Sprintf("%d", limit))

	case "unread_messages":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for unread_messages action")
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "unread", args.Channel)

	default:
		return "", fmt.Errorf("unknown channels action: %s", args.Action)
	}
}
