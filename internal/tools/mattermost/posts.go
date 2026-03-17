package mattermost

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type postsTool struct {
	binary string
	runner commandRunner
}

func (self *postsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "mattermost_posts",
			Description: "Interact with Mattermost posts/messages. Actions: create (post a message), " +
				"reply (reply in a thread), edit (edit a post), delete (delete a post), " +
				"thread (view a thread), search (search for posts), " +
				"react (add reaction), dm (send direct message).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"create", "reply", "edit", "delete", "thread", "search", "react", "dm"},
						"description": "The post action to perform.",
					},
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Channel name or ID (for 'create' action).",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Message text (for 'create', 'reply', 'edit', 'dm' actions).",
					},
					"post_id": map[string]interface{}{
						"type":        "string",
						"description": "Post ID (for 'reply', 'edit', 'delete', 'thread', 'react' actions).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action).",
					},
					"emoji": map[string]interface{}{
						"type":        "string",
						"description": "Emoji name without colons (for 'react' action).",
					},
					"username": map[string]interface{}{
						"type":        "string",
						"description": "Username to send direct message to (for 'dm' action).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *postsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"thread", "search"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *postsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action   string `json:"action"`
		Channel  string `json:"channel"`
		Message  string `json:"message"`
		PostID   string `json:"post_id"`
		Query    string `json:"query"`
		Emoji    string `json:"emoji"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "create":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for create action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for create action")
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "create", args.Channel, args.Message)

	case "reply":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for reply action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for reply action")
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "reply", args.PostID, args.Message)

	case "edit":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for edit action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for edit action")
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "edit", args.PostID, args.Message)

	case "delete":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for delete action")
		}
		output, err := execMattermost(ctx, self.runner, self.binary, "post", "delete", args.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	case "thread":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for thread action")
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "thread", args.PostID)

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		return execMattermost(ctx, self.runner, self.binary, "post", "search", args.Query)

	case "react":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for react action")
		}
		if args.Emoji == "" {
			return "", fmt.Errorf("emoji is required for react action")
		}
		output, err := execMattermost(ctx, self.runner, self.binary, "post", "react", args.PostID, args.Emoji)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reacted", output), nil

	case "dm":
		if args.Username == "" {
			return "", fmt.Errorf("username is required for dm action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for dm action")
		}
		return execMattermost(ctx, self.runner, self.binary, "dm", "send", args.Username, args.Message)

	default:
		return "", fmt.Errorf("unknown posts action: %s", args.Action)
	}
}
