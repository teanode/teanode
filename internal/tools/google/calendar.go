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

type calendarTool struct {
	binary string

	runner commandRunner
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

func (self *calendarTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list", "search"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *calendarTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
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
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("google: parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		days := arguments.Days
		if days <= 0 {
			days = 7
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"calendar", "events", "primary", "--days", strconv.Itoa(days))

	case "search":
		if arguments.Query == "" {
			return "", fmt.Errorf("google: query is required for search action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"calendar", "search", arguments.Query)

	case "create":
		if arguments.Summary == "" {
			return "", fmt.Errorf("google: summary is required for create action")
		}
		if arguments.From == "" {
			return "", fmt.Errorf("google: from is required for create action")
		}
		if arguments.To == "" {
			return "", fmt.Errorf("google: to is required for create action")
		}
		commandArguments := []string{"calendar", "create", "primary",
			"--summary", arguments.Summary, "--from", arguments.From, "--to", arguments.To}
		if arguments.Description != "" {
			commandArguments = append(commandArguments, "--description", arguments.Description)
		}
		if arguments.Attendees != "" {
			commandArguments = append(commandArguments, "--attendees", arguments.Attendees)
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account, commandArguments...)

	case "delete":
		if arguments.EventID == "" {
			return "", fmt.Errorf("google: event_id is required for delete action")
		}
		return execGog(ctx, self.runner, self.binary, configurationFromContext(ctx).account,
			"calendar", "delete", "primary", arguments.EventID)

	default:
		return "", fmt.Errorf("google: unknown calendar action: %s", arguments.Action)
	}
}
