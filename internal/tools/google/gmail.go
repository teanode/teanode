package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

type gmailTool struct {
	binary string

	runner commandRunner
}

func (self *gmailTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "google_gmail",
			Description: "Interact with Gmail. Actions: search (find emails), read (get email content), " +
				"send (compose new email), reply (reply to email), trash (delete email).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"search", "read", "send", "reply", "trash"},
						"description": "The Gmail action to perform.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action). Supports Gmail search syntax.",
					},
					"message_id": map[string]interface{}{
						"type":        "string",
						"description": "Message ID (for 'read', 'reply', 'trash' actions).",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "Recipient email address (for 'send' action).",
					},
					"subject": map[string]interface{}{
						"type":        "string",
						"description": "Email subject (for 'send' action).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Email body text (for 'send' and 'reply' actions).",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (for 'search' action, default 10).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *gmailTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"search", "read"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *gmailTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action    string `json:"action"`
		Query     string `json:"query"`
		MessageID string `json:"message_id"`
		To        string `json:"to"`
		Subject   string `json:"subject"`
		Body      string `json:"body"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "search", args.Query, "--max", strconv.Itoa(limit))

	case "read":
		if args.MessageID == "" {
			return "", fmt.Errorf("message_id is required for read action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "get", args.MessageID)

	case "send":
		if args.To == "" {
			return "", fmt.Errorf("to is required for send action")
		}
		if args.Subject == "" {
			return "", fmt.Errorf("subject is required for send action")
		}
		if args.Body == "" {
			return "", fmt.Errorf("body is required for send action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "send", "--to", args.To, "--subject", args.Subject, "--body", args.Body)

	case "reply":
		if args.MessageID == "" {
			return "", fmt.Errorf("message_id is required for reply action")
		}
		if args.Body == "" {
			return "", fmt.Errorf("body is required for reply action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "send", "--reply-to-message-id", args.MessageID, "--body", args.Body)

	case "trash":
		if args.MessageID == "" {
			return "", fmt.Errorf("message_id is required for trash action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "thread", "modify", args.MessageID, "--add", "TRASH")

	default:
		return "", fmt.Errorf("unknown gmail action: %s", args.Action)
	}
}
