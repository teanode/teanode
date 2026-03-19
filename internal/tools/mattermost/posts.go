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
	var args struct {
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
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	exec := func(args2 ...string) (string, error) {
		return execMattermostWithTeam(ctx, self.runner, self.binary, args.Team, args2...)
	}

	switch args.Action {
	case "create":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for create action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for create action")
		}
		return exec("post", "create", args.Channel, args.Message)

	case "reply":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for reply action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for reply action")
		}
		return exec("post", "reply", args.PostID, args.Message)

	case "edit":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for edit action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for edit action")
		}
		return exec("post", "edit", args.PostID, args.Message)

	case "delete":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for delete action")
		}
		output, err := exec("post", "delete", args.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	case "thread":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for thread action")
		}
		return exec("post", "thread", args.PostID)

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		if args.ORSearch {
			return exec("post", "search", args.Query, "--or")
		}
		return exec("post", "search", args.Query)

	case "pinned":
		if args.Channel == "" {
			return "", fmt.Errorf("channel is required for pinned action")
		}
		return exec("post", "pinned", args.Channel)

	case "react":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for react action")
		}
		if args.Emoji == "" {
			return "", fmt.Errorf("emoji is required for react action")
		}
		output, err := exec("post", "react", args.PostID, args.Emoji)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("reacted", output), nil

	case "unreact":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for unreact action")
		}
		if args.Emoji == "" {
			return "", fmt.Errorf("emoji is required for unreact action")
		}
		output, err := exec("post", "unreact", args.PostID, args.Emoji)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unreacted", output), nil

	case "pin":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for pin action")
		}
		output, err := exec("post", "pin", args.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("pinned", output), nil

	case "unpin":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for unpin action")
		}
		output, err := exec("post", "unpin", args.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unpinned", output), nil

	case "history":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for history action")
		}
		return exec("post", "history", args.PostID)

	case "saved_list":
		commandArgs := []string{"saved", "list"}
		if args.Channel != "" {
			commandArgs = append(commandArgs, "--channel", args.Channel)
		}
		if args.Limit > 0 {
			commandArgs = append(commandArgs, "-n", fmt.Sprintf("%d", args.Limit))
		}
		return exec(commandArgs...)

	case "saved_add":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for saved_add action")
		}
		output, err := exec("saved", "add", args.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("saved", output), nil

	case "saved_remove":
		if args.PostID == "" {
			return "", fmt.Errorf("post_id is required for saved_remove action")
		}
		output, err := exec("saved", "remove", args.PostID)
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("unsaved", output), nil

	case "dm":
		if args.Username == "" {
			return "", fmt.Errorf("username is required for dm action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for dm action")
		}
		return exec("dm", "send", args.Username, args.Message)

	case "dm_read":
		if args.Username == "" {
			return "", fmt.Errorf("username is required for dm_read action")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 20
		}
		return exec("dm", "read", args.Username, "-n", fmt.Sprintf("%d", limit))

	case "dm_list":
		return exec("dm", "list")

	case "dm_group":
		if len(args.Usernames) == 0 {
			return "", fmt.Errorf("usernames is required for dm_group action")
		}
		if args.Message == "" {
			return "", fmt.Errorf("message is required for dm_group action")
		}
		return exec("dm", "group", strings.Join(args.Usernames, ","), args.Message)

	default:
		return "", fmt.Errorf("unknown posts action: %s", args.Action)
	}
}
