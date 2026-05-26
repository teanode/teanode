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
	var arguments struct {
		Action    string `json:"action"`
		Query     string `json:"query"`
		MessageID string `json:"message_id"`
		To        string `json:"to"`
		Subject   string `json:"subject"`
		Body      string `json:"body"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("google: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("google: query is required for search action")
		}
		limit := arguments.Limit
		if limit <= 0 {
			limit = 10
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "search", arguments.Query, "--max", strconv.Itoa(limit))

	case "read":
		if arguments.MessageID == "" {
			return "", fmt.Errorf("google: message_id is required for read action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "get", arguments.MessageID)

	case "send":
		if arguments.To == "" {
			return "", fmt.Errorf("google: to is required for send action")
		}
		if arguments.Subject == "" {
			return "", fmt.Errorf("google: subject is required for send action")
		}
		if arguments.Body == "" {
			return "", fmt.Errorf("google: body is required for send action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "send", "--to", arguments.To, "--subject", arguments.Subject, "--body", arguments.Body)

	case "reply":
		if arguments.MessageID == "" {
			return "", fmt.Errorf("google: message_id is required for reply action")
		}
		if arguments.Body == "" {
			return "", fmt.Errorf("google: body is required for reply action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "send", "--reply-to-message-id", arguments.MessageID, "--body", arguments.Body)

	case "trash":
		if arguments.MessageID == "" {
			return "", fmt.Errorf("google: message_id is required for trash action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"gmail", "thread", "modify", arguments.MessageID, "--add", "TRASH")

	default:
		return "", fmt.Errorf("google: unknown gmail action: %s", arguments.Action)
	}
}
