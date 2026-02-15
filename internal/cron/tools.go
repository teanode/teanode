package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/teanode/teanode/internal/agent"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterCronTools adds cron management tools to the registry.
func RegisterCronTools(registry *agent.ToolRegistry, scheduler *Scheduler) {
	registry.Register(&cronListTool{scheduler: scheduler})
	registry.Register(&cronCreateTool{scheduler: scheduler})
	registry.Register(&cronUpdateTool{scheduler: scheduler})
	registry.Register(&cronDeleteTool{scheduler: scheduler})
	registry.Register(&cronTriggerTool{scheduler: scheduler})
}

// --- cron_list ---

type cronListTool struct{ scheduler *Scheduler }

func (self *cronListTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "cron_list",
			Description: "List all cron jobs and one-shot reminders with their schedule, status, and last run info.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (self *cronListTool) Execute(_ context.Context, _ string) (string, error) {
	jobs := self.scheduler.List()
	result, _ := json.Marshal(map[string]interface{}{
		"jobs": jobs,
	})
	return string(result), nil
}

// --- cron_create ---

type cronCreateTool struct{ scheduler *Scheduler }

func (self *cronCreateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "cron_create",
			Description: "Create a new cron job. Use 'schedule' (cron expression) for recurring jobs, or 'delay' (e.g. '1h', '30m') for one-shot reminders. " +
				"When using 'delay', the job fires once after the specified time in the current conversation session and auto-deletes. " +
				"Examples: schedule='0 9 * * *' for daily at 9am, delay='1h' to remind in 1 hour, delay='30m' to remind in 30 minutes.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name for the cron job.",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron expression (5-field: minute hour day-of-month month day-of-week). Example: '0 9 * * 1-5' for 9am weekdays. Mutually exclusive with 'delay'.",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The prompt/message to send when the job triggers.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model override for this job.",
					},
					"agentId": map[string]interface{}{
						"type":        "string",
						"description": "Optional agent ID to run this job against. Defaults to 'main'.",
					},
					"delay": map[string]interface{}{
						"type":        "string",
						"description": "One-shot delay instead of a recurring schedule. Go duration format: '30m', '1h', '2h30m'. When set, the job fires once after this delay in the current conversation session and then self-destructs. Mutually exclusive with 'schedule'.",
					},
					"oneShot": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, the job auto-deletes after its first execution. Automatically set when using 'delay'.",
					},
				},
				"required": []string{"name", "message"},
			},
		},
	}
}

func (self *cronCreateTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Message  string `json:"message"`
		Model    string `json:"model"`
		AgentID  string `json:"agentId"`
		Delay    string `json:"delay"`
		OneShot  *bool  `json:"oneShot"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Name == "" || arguments.Message == "" {
		return "", fmt.Errorf("name and message are required")
	}
	if arguments.Delay != "" && arguments.Schedule != "" {
		return "", fmt.Errorf("provide either 'schedule' or 'delay', not both")
	}
	if arguments.Delay == "" && arguments.Schedule == "" {
		return "", fmt.Errorf("either 'schedule' or 'delay' is required")
	}

	var runAt int64
	oneShot := false
	sessionKey := uuid.New().String()

	if arguments.Delay != "" {
		duration, parseError := time.ParseDuration(arguments.Delay)
		if parseError != nil {
			return "", fmt.Errorf("invalid delay %q: %w (use Go duration format: '30m', '1h', '2h30m')", arguments.Delay, parseError)
		}
		if duration < time.Minute {
			return "", fmt.Errorf("delay must be at least 1 minute, got %s", duration)
		}
		runAt = time.Now().Add(duration).UnixMilli()
		oneShot = true
		// Bind to the current conversation session.
		if contextSessionKey := agent.SessionKeyFromContext(ctx); contextSessionKey != "" {
			sessionKey = contextSessionKey
		}
	} else {
		if _, parseError := Parse(arguments.Schedule); parseError != nil {
			return "", fmt.Errorf("invalid cron expression: %w", parseError)
		}
	}

	if arguments.OneShot != nil {
		oneShot = *arguments.OneShot
	}

	job := CronJob{
		ID:         uuid.New().String(),
		Name:       arguments.Name,
		Schedule:   arguments.Schedule,
		Message:    arguments.Message,
		Model:      arguments.Model,
		AgentID:    arguments.AgentID,
		Enabled:    true,
		SessionKey: sessionKey,
		RunAt:      runAt,
		OneShot:    oneShot,
		CreatedAt:  time.Now().UnixMilli(),
	}

	if err := self.scheduler.store.Create(job); err != nil {
		return "", fmt.Errorf("creating job: %w", err)
	}
	if err := self.scheduler.Reload(); err != nil {
		return "", fmt.Errorf("reloading scheduler: %w", err)
	}

	response := map[string]interface{}{
		"id":      job.ID,
		"name":    job.Name,
		"agentId": job.AgentID,
	}
	if job.Schedule != "" {
		response["schedule"] = job.Schedule
	}
	if job.RunAt > 0 {
		response["firesAt"] = time.UnixMilli(job.RunAt).Format(time.RFC3339)
	}
	result, _ := json.Marshal(response)
	return string(result), nil
}

// --- cron_update ---

type cronUpdateTool struct{ scheduler *Scheduler }

func (self *cronUpdateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "cron_update",
			Description: "Update an existing cron job or reminder.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the cron job to update.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "New name (optional).",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "New cron expression (optional).",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "New message (optional).",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "New model override (optional).",
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable or disable the job (optional).",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (self *cronUpdateTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Message  string `json:"message"`
		Model    string `json:"model"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	// Find existing job.
	jobs := self.scheduler.List()
	var job *CronJob
	for index := range jobs {
		if jobs[index].ID == arguments.ID {
			job = &jobs[index]
			break
		}
	}
	if job == nil {
		return "", fmt.Errorf("job not found: %s", arguments.ID)
	}

	if arguments.Name != "" {
		job.Name = arguments.Name
	}
	if arguments.Schedule != "" {
		if _, err := Parse(arguments.Schedule); err != nil {
			return "", fmt.Errorf("invalid cron expression: %w", err)
		}
		job.Schedule = arguments.Schedule
	}
	if arguments.Message != "" {
		job.Message = arguments.Message
	}
	if arguments.Model != "" {
		job.Model = arguments.Model
	}
	if arguments.Enabled != nil {
		job.Enabled = *arguments.Enabled
	}

	if err := self.scheduler.store.Update(*job); err != nil {
		return "", fmt.Errorf("updating job: %w", err)
	}
	if err := self.scheduler.Reload(); err != nil {
		return "", fmt.Errorf("reloading scheduler: %w", err)
	}

	return fmt.Sprintf("Updated cron job '%s' (id: %s).", job.Name, job.ID), nil
}

// --- cron_delete ---

type cronDeleteTool struct{ scheduler *Scheduler }

func (self *cronDeleteTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "cron_delete",
			Description: "Delete a cron job or cancel a pending reminder.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the cron job to delete.",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (self *cronDeleteTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	if err := self.scheduler.store.Delete(arguments.ID); err != nil {
		return "", fmt.Errorf("deleting job: %w", err)
	}
	if err := self.scheduler.Reload(); err != nil {
		return "", fmt.Errorf("reloading scheduler: %w", err)
	}

	return fmt.Sprintf("Deleted cron job %s.", arguments.ID), nil
}

// --- cron_trigger ---

type cronTriggerTool struct{ scheduler *Scheduler }

func (self *cronTriggerTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "cron_trigger",
			Description: "Manually trigger a cron job to run immediately.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the cron job to trigger.",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (self *cronTriggerTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	if err := self.scheduler.Trigger(arguments.ID); err != nil {
		return "", err
	}

	return fmt.Sprintf("Triggered cron job %s. It will run in the background.", arguments.ID), nil
}

// truncateString truncates a string to maxLength, appending "..." if truncated.
func truncateString(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "..."
}
