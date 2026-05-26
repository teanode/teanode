package mattermost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
				"thread (view a thread), search (search for posts), pinned (list pinned posts), " +
				"pin/unpin (pin or unpin a post), react/unreact (manage reactions), " +
				"history (show edit history), saved_list/saved_add/saved_remove (manage saved posts), " +
				"dm (send direct message), dm_read (read DM history), " +
				"dm_list (list DM conversations), dm_group (send a group DM).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"create", "reply", "edit", "delete", "thread", "search", "pinned", "pin", "unpin", "react", "unreact", "history", "saved_list", "saved_add", "saved_remove", "dm", "dm_read", "dm_list", "dm_group"},
						"description": "The post action to perform.",
					},
					"channel": map[string]interface{}{
						"type":        "string",
						"description": "Channel name or ID (for 'create' and 'pinned' actions).",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Message text (for 'create', 'reply', 'edit', 'dm', 'dm_group' actions).",
					},
					"post_id": map[string]interface{}{
						"type":        "string",
						"description": "Post ID (for 'reply', 'edit', 'delete', 'thread', 'react', 'unreact' actions).",
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
						"description": "Username to send or read a direct message with (for 'dm' and 'dm_read' actions).",
					},
					"usernames": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Usernames for a group DM (for 'dm_group' action).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (for 'dm_read' action, default 20).",
					},
					"or_search": map[string]interface{}{
						"type":        "boolean",
						"description": "Use OR instead of AND between search terms (for 'search' action).",
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

func (self *postsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"thread", "search", "pinned", "history", "saved_list", "dm_read", "dm_list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *postsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action    string   `json:"action"`
		Channel   string   `json:"channel"`
		Message   string   `json:"message"`
		PostID    string   `json:"post_id"`
		Query     string   `json:"query"`
		Emoji     string   `json:"emoji"`
		Username  string   `json:"username"`
		Usernames []string `json:"usernames"`
		Limit     int      `json:"limit"`
		ORSearch  bool     `json:"or_search"`
		Team      string   `json:"team"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("mattermost: parsing arguments: %w", err)
	}

	exec := func(arguments2 ...string) (string, error) {
		return execMattermostWithTeam(ctx, self.runner, self.binary, arguments.Team, arguments2...)
	}

	switch arguments.Action {
	case "create":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for create action")
		}
		if arguments.Message == "" {
			return "", fmt.Errorf("mattermost: message is required for create action")
		}
		return exec("post", "create", arguments.Channel, arguments.Message)

	case "reply":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for reply action")
		}
		if arguments.Message == "" {
			return "", fmt.Errorf("mattermost: message is required for reply action")
		}
		return exec("post", "reply", arguments.PostID, arguments.Message)

	case "edit":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for edit action")
		}
		if arguments.Message == "" {
			return "", fmt.Errorf("mattermost: message is required for edit action")
		}
		return exec("post", "edit", arguments.PostID, arguments.Message)

	case "delete":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for delete action")
		}
		output, err := exec("post", "delete", arguments.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	case "thread":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for thread action")
		}
		return exec("post", "thread", arguments.PostID)

	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("mattermost: query is required for search action")
		}
		if arguments.ORSearch {
			return exec("post", "search", arguments.Query, "--or")
		}
		return exec("post", "search", arguments.Query)

	case "pinned":
		if arguments.Channel == "" {
			return "", fmt.Errorf("mattermost: channel is required for pinned action")
		}
		return exec("post", "pinned", arguments.Channel)

	case "react":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for react action")
		}
		if arguments.Emoji == "" {
			return "", fmt.Errorf("mattermost: emoji is required for react action")
		}
		output, err := exec("post", "react", arguments.PostID, arguments.Emoji)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reacted", output), nil

	case "unreact":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for unreact action")
		}
		if arguments.Emoji == "" {
			return "", fmt.Errorf("mattermost: emoji is required for unreact action")
		}
		output, err := exec("post", "unreact", arguments.PostID, arguments.Emoji)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unreacted", output), nil

	case "pin":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for pin action")
		}
		output, err := exec("post", "pin", arguments.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("pinned", output), nil

	case "unpin":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for unpin action")
		}
		output, err := exec("post", "unpin", arguments.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unpinned", output), nil

	case "history":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for history action")
		}
		return exec("post", "history", arguments.PostID)

	case "saved_list":
		commandArguments := []string{"saved", "list"}
		if arguments.Channel != "" {
			commandArguments = append(commandArguments, "--channel", arguments.Channel)
		}
		if arguments.Limit > 0 {
			commandArguments = append(commandArguments, "-n", fmt.Sprintf("%d", arguments.Limit))
		}
		return exec(commandArguments...)

	case "saved_add":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for saved_add action")
		}
		output, err := exec("saved", "add", arguments.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("saved", output), nil

	case "saved_remove":
		if arguments.PostID == "" {
			return "", fmt.Errorf("mattermost: post_id is required for saved_remove action")
		}
		output, err := exec("saved", "remove", arguments.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unsaved", output), nil

	case "dm":
		if arguments.Username == "" {
			return "", fmt.Errorf("mattermost: username is required for dm action")
		}
		if arguments.Message == "" {
			return "", fmt.Errorf("mattermost: message is required for dm action")
		}
		return exec("dm", "send", arguments.Username, arguments.Message)

	case "dm_read":
		if arguments.Username == "" {
			return "", fmt.Errorf("mattermost: username is required for dm_read action")
		}
		limit := arguments.Limit
		if limit <= 0 {
			limit = 20
		}
		return exec("dm", "read", arguments.Username, "-n", fmt.Sprintf("%d", limit))

	case "dm_list":
		return exec("dm", "list")

	case "dm_group":
		if len(arguments.Usernames) == 0 {
			return "", fmt.Errorf("mattermost: usernames is required for dm_group action")
		}
		if arguments.Message == "" {
			return "", fmt.Errorf("mattermost: message is required for dm_group action")
		}
		return exec("dm", "group", strings.Join(arguments.Usernames, ","), arguments.Message)

	default:
		return "", fmt.Errorf("mattermost: unknown posts action: %s", arguments.Action)
	}
}
