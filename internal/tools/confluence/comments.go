package confluence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type commentsTool struct {
	binary string
	runner commandRunner
}

func (self *commentsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "confluence_comments",
			Description: "Interact with Confluence page comments. Actions: list (list comments on a page), " +
				"create (add a comment to a page), reply (reply to a comment), delete (delete a comment).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "create", "reply", "delete"},
						"description": "The comment action to perform.",
					},
					"page_id": map[string]interface{}{
						"type":        "string",
						"description": "Page ID or URL (for 'list', 'create', 'reply' actions).",
					},
					"comment_id": map[string]interface{}{
						"type":        "string",
						"description": "Comment ID (for 'reply' and 'delete' actions).",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Comment content (for 'create' and 'reply' actions).",
					},
					"content_format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"storage", "html", "markdown"},
						"description": "Content format (for 'create' and 'reply' actions; default: storage).",
					},
					"location": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"inline", "footer", "resolved"},
						"description": "Filter by comment location (for 'list' action) or set location (for 'create' action; default: footer).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of comments to return (for 'list' action, default 25).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *commentsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *commentsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action        string `json:"action"`
		PageID        string `json:"page_id"`
		CommentID     string `json:"comment_id"`
		Content       string `json:"content"`
		ContentFormat string `json:"content_format"`
		Location      string `json:"location"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for list action")
		}
		commandArgs := []string{"comments", args.PageID}
		if args.Location != "" {
			commandArgs = append(commandArgs, "--location", args.Location)
		}
		if args.Limit > 0 {
			commandArgs = append(commandArgs, "--limit", fmt.Sprintf("%d", args.Limit))
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "create":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for create action")
		}
		if args.Content == "" {
			return "", fmt.Errorf("content is required for create action")
		}
		commandArgs := []string{"comment", args.PageID, "--content", args.Content}
		if args.ContentFormat != "" {
			commandArgs = append(commandArgs, "--format", args.ContentFormat)
		}
		if args.Location != "" {
			commandArgs = append(commandArgs, "--location", args.Location)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "reply":
		if args.PageID == "" {
			return "", fmt.Errorf("page_id is required for reply action")
		}
		if args.CommentID == "" {
			return "", fmt.Errorf("comment_id is required for reply action")
		}
		if args.Content == "" {
			return "", fmt.Errorf("content is required for reply action")
		}
		commandArgs := []string{"comment", args.PageID, "--content", args.Content, "--parent", args.CommentID}
		if args.ContentFormat != "" {
			commandArgs = append(commandArgs, "--format", args.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArgs...)

	case "delete":
		if args.CommentID == "" {
			return "", fmt.Errorf("comment_id is required for delete action")
		}
		output, err := execConfluence(ctx, self.runner, self.binary, "comment-delete", args.CommentID, "--yes")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	default:
		return "", fmt.Errorf("unknown comments action: %s", args.Action)
	}
}
