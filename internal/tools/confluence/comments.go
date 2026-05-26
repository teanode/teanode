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
	var arguments struct {
		Action        string `json:"action"`
		PageID        string `json:"page_id"`
		CommentID     string `json:"comment_id"`
		Content       string `json:"content"`
		ContentFormat string `json:"content_format"`
		Location      string `json:"location"`
		Limit         int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("confluence: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for list action")
		}
		commandArguments := []string{"comments", arguments.PageID}
		if arguments.Location != "" {
			commandArguments = append(commandArguments, "--location", arguments.Location)
		}
		if arguments.Limit > 0 {
			commandArguments = append(commandArguments, "--limit", fmt.Sprintf("%d", arguments.Limit))
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "create":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for create action")
		}
		if arguments.Content == "" {
			return "", fmt.Errorf("confluence: content is required for create action")
		}
		commandArguments := []string{"comment", arguments.PageID, "--content", arguments.Content}
		if arguments.ContentFormat != "" {
			commandArguments = append(commandArguments, "--format", arguments.ContentFormat)
		}
		if arguments.Location != "" {
			commandArguments = append(commandArguments, "--location", arguments.Location)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "reply":
		if arguments.PageID == "" {
			return "", fmt.Errorf("confluence: page_id is required for reply action")
		}
		if arguments.CommentID == "" {
			return "", fmt.Errorf("confluence: comment_id is required for reply action")
		}
		if arguments.Content == "" {
			return "", fmt.Errorf("confluence: content is required for reply action")
		}
		commandArguments := []string{"comment", arguments.PageID, "--content", arguments.Content, "--parent", arguments.CommentID}
		if arguments.ContentFormat != "" {
			commandArguments = append(commandArguments, "--format", arguments.ContentFormat)
		}
		return execConfluence(ctx, self.runner, self.binary, commandArguments...)

	case "delete":
		if arguments.CommentID == "" {
			return "", fmt.Errorf("confluence: comment_id is required for delete action")
		}
		output, err := execConfluence(ctx, self.runner, self.binary, "comment-delete", arguments.CommentID, "--yes")
		if err != nil {
			return "", err
		}
		return wrapPlainOutput("deleted", output), nil

	default:
		return "", fmt.Errorf("confluence: unknown comments action: %s", arguments.Action)
	}
}
