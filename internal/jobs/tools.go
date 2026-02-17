package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/provider"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/ulid"
)

// RegisterTools adds job management tools to the registry.
func RegisterTools(registry *agents.ToolRegistry, scheduler *Scheduler) {
	registry.Register(&jobListTool{scheduler: scheduler})
	registry.Register(&jobCreateTool{scheduler: scheduler})
	registry.Register(&jobUpdateTool{scheduler: scheduler})
	registry.Register(&jobDeleteTool{scheduler: scheduler})
	registry.Register(&jobTriggerTool{scheduler: scheduler})
}

// --- jobs_list ---

type jobListTool struct{ scheduler *Scheduler }

func (self *jobListTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "jobs_list",
			Description: "List all scheduled jobs and one-shot reminders with their schedule, status, and last run info.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (self *jobListTool) Execute(_ context.Context, _ string) (string, error) {
	jobs := self.scheduler.List()
	result, _ := json.Marshal(map[string]interface{}{
		"jobs": jobs,
	})
	return string(result), nil
}

// --- jobs_create ---

type jobCreateTool struct{ scheduler *Scheduler }

func (self *jobCreateTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "jobs_create",
			Description: "Create a new scheduled job. Use 'schedule' (cron expression) for recurring jobs, or 'delay' (e.g. '1h', '30m') for one-shot reminders. " +
				"When using 'delay', the job fires once after the specified time in the current conversation and auto-deletes. " +
				"Examples: schedule='0 9 * * *' for daily at 9am, delay='1h' to remind in 1 hour, delay='30m' to remind in 30 minutes.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name for the job.",
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
						"description": "One-shot delay instead of a recurring schedule. Go duration format: '30m', '1h', '2h30m'. When set, the job fires once after this delay in the current conversation and then self-destructs. Mutually exclusive with 'schedule'.",
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

func (self *jobCreateTool) Execute(ctx context.Context, rawArguments string) (string, error) {
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
	conversationId := "" // recurring jobs resolve conversation at execution time

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
		// One-shot reminders bind to the current conversation.
		if contextConversationId := agents.ConversationIDFromContext(ctx); contextConversationId != "" {
			conversationId = contextConversationId
		} else {
			conversationId = ulid.GenerateString()
		}
	} else {
		if _, parseError := cronexpr.Parse(arguments.Schedule); parseError != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", parseError)
		}
	}

	if arguments.OneShot != nil {
		oneShot = *arguments.OneShot
	}

	job := Job{
		ID:             ulid.GenerateString(),
		Name:           arguments.Name,
		Schedule:       arguments.Schedule,
		Message:        arguments.Message,
		Model:          arguments.Model,
		AgentID:        arguments.AgentID,
		Enabled:        true,
		ConversationID: conversationId,
		RunAt:          runAt,
		OneShot:        oneShot,
		CreatedAt:      time.Now().UnixMilli(),
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

// --- jobs_update ---

type jobUpdateTool struct{ scheduler *Scheduler }

func (self *jobUpdateTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "jobs_update",
			Description: "Update an existing scheduled job or reminder.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the job to update.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "New name (optional).",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "New schedule expression (optional).",
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

func (self *jobUpdateTool) Execute(_ context.Context, rawArguments string) (string, error) {
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
	var job *Job
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
		if _, err := cronexpr.Parse(arguments.Schedule); err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
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

	return fmt.Sprintf("Updated job '%s' (id: %s).", job.Name, job.ID), nil
}

// --- jobs_delete ---

type jobDeleteTool struct{ scheduler *Scheduler }

func (self *jobDeleteTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "jobs_delete",
			Description: "Delete a scheduled job or cancel a pending reminder.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the job to delete.",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (self *jobDeleteTool) Execute(_ context.Context, rawArguments string) (string, error) {
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

	return fmt.Sprintf("Deleted job %s.", arguments.ID), nil
}

// --- jobs_trigger ---

type jobTriggerTool struct{ scheduler *Scheduler }

func (self *jobTriggerTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "jobs_trigger",
			Description: "Manually trigger a scheduled job to run immediately.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the job to trigger.",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

func (self *jobTriggerTool) Execute(_ context.Context, rawArguments string) (string, error) {
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

	return fmt.Sprintf("Triggered job %s. It will run in the background.", arguments.ID), nil
}

// truncateString truncates a string to maxLength, appending "..." if truncated.
func truncateString(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "..."
}
