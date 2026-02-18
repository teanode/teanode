package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/teanode/teanode/internal/providers"
)

type calendarTool struct {
	binary  string
	account string
	runner  commandRunner
}

func (self *calendarTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "google_calendar",
			Description: "Interact with Google Calendar. Actions: list (upcoming events), search (find events), " +
				"create (new event), delete (remove event).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "search", "create", "delete"},
						"description": "The Calendar action to perform.",
					},
					"days": map[string]interface{}{
						"type":        "integer",
						"description": "Number of days to look ahead (for 'list' action, default 7).",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query (for 'search' action).",
					},
					"summary": map[string]interface{}{
						"type":        "string",
						"description": "Event title (for 'create' action).",
					},
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Start time in ISO 8601 or natural language (for 'create' action).",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "End time in ISO 8601 or natural language (for 'create' action).",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Event description (for 'create' action, optional).",
					},
					"attendees": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated attendee email addresses (for 'create' action, optional).",
					},
					"event_id": map[string]interface{}{
						"type":        "string",
						"description": "Event ID (for 'delete' action).",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (self *calendarTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var args struct {
		Action      string `json:"action"`
		Days        int    `json:"days"`
		Query       string `json:"query"`
		Summary     string `json:"summary"`
		From        string `json:"from"`
		To          string `json:"to"`
		Description string `json:"description"`
		Attendees   string `json:"attendees"`
		EventID     string `json:"event_id"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch args.Action {
	case "list":
		days := args.Days
		if days <= 0 {
			days = 7
		}
		return execGog(ctx, self.runner, self.binary, self.account,
			"calendar", "events", "primary", "--days", strconv.Itoa(days))

	case "search":
		if args.Query == "" {
			return "", fmt.Errorf("query is required for search action")
		}
		return execGog(ctx, self.runner, self.binary, self.account,
			"calendar", "search", args.Query)

	case "create":
		if args.Summary == "" {
			return "", fmt.Errorf("summary is required for create action")
		}
		if args.From == "" {
			return "", fmt.Errorf("from is required for create action")
		}
		if args.To == "" {
			return "", fmt.Errorf("to is required for create action")
		}
		cmdArgs := []string{"calendar", "create", "primary",
			"--summary", args.Summary, "--from", args.From, "--to", args.To}
		if args.Description != "" {
			cmdArgs = append(cmdArgs, "--description", args.Description)
		}
		if args.Attendees != "" {
			cmdArgs = append(cmdArgs, "--attendees", args.Attendees)
		}
		return execGog(ctx, self.runner, self.binary, self.account, cmdArgs...)

	case "delete":
		if args.EventID == "" {
			return "", fmt.Errorf("event_id is required for delete action")
		}
		return execGog(ctx, self.runner, self.binary, self.account,
			"calendar", "delete", "primary", args.EventID)

	default:
		return "", fmt.Errorf("unknown calendar action: %s", args.Action)
	}
}
