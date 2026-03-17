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
			Description: "Interact with Mattermost channels. Actions: list (list joined channels), " +
				"info (get channel details), unread (list channels with unread messages), " +
				"read_messages (read recent messages in a channel), unread_messages (read unread messages in a channel).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "info", "unread", "read_messages", "unread_messages"},
						"description": "The channel action to perform.",
					},
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Channel name or ID (for 'info', 'read_messages', 'unread_messages' actions).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of messages to return (for 'read_messages' action, default 20).",
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
		Action  string `json:"action"`
		Channel string `json:"channel"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		return execMattermost(ctx, self.runner, self.binary, "channel", "list")

	case "info":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for info action")
		}
		return execMattermost(ctx, self.runner, self.binary, "channel", "info", args.Channel)

	case "unread":
		return execMattermost(ctx, self.runner, self.binary, "channel", "unread")

	case "read_messages":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for read_messages action")
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
