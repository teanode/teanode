package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/security"
)

// RegisterTools adds job management tools to the registry.
func RegisterTools(registry *agents.ToolRegistry, scheduler *Scheduler) {
	registry.Register(&jobsTool{scheduler: scheduler})
}

// --- jobs (multi-action) ---

type jobsTool struct{ scheduler *Scheduler }

func (self *jobsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "jobs",
			Description: "Manage scheduled jobs and one-shot reminders. Actions: list (view all jobs), " +
				"create (new job or reminder), update (modify existing job), delete (remove a job), " +
				"trigger (run a job immediately).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "create", "update", "delete", "trigger"},
						"description": "The job action to perform.",
					},
					"id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the job (for update, delete, trigger actions).",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name for the job (required for create, optional for update).",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron expression, 5-field: minute hour day-of-month month day-of-week. Example: '0 9 * * 1-5' for 9am weekdays. Mutually exclusive with 'delay' (for create, update actions).",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "The prompt/message to send when the job triggers (required for create, optional for update).",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model override for this job (for create, update actions).",
					},
					"agentId": map[string]interface{}{
						"type":        "string",
						"description": "Agent ID to run this job against, defaults to 'main' (for create action).",
					},
					"delay": map[string]interface{}{
						"type":        "string",
						"description": "One-shot delay instead of recurring schedule. Go duration format: '30m', '1h', '2h30m'. Fires once then self-destructs. Mutually exclusive with 'schedule' (for create action).",
					},
					"oneShot": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, auto-delete after first execution. Automatically set when using 'delay' (for create action).",
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable or disable the job (for update action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action":  map[string]interface{}{"type": "string"},
					"jobs":    map[string]interface{}{"type": "array", "description": "List of jobs (for list action)"},
					"id":      map[string]interface{}{"type": "string"},
					"name":    map[string]interface{}{"type": "string"},
					"success": map[string]interface{}{"type": "boolean", "description": "Whether the action succeeded (for update, delete, trigger actions)"},
				},
			},
		},
	}
}

func (self *jobsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action   string `json:"action"`
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Message  string `json:"message"`
		Model    string `json:"model"`
		AgentID  string `json:"agentId"`
		Delay    string `json:"delay"`
		OneShot  *bool  `json:"oneShot"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	userId := agents.UserIDFromContext(ctx)
	if userId == "" {
		return "", fmt.Errorf("authenticated user context is required")
	}

	switch arguments.Action {
	case "list":
		return self.executeList(userId)
	case "create":
		return self.executeCreate(ctx, userId, arguments.Name, arguments.Schedule, arguments.Message, arguments.Model, arguments.AgentID, arguments.Delay, arguments.OneShot)
	case "update":
		return self.executeUpdate(userId, arguments.ID, arguments.Name, arguments.Schedule, arguments.Message, arguments.Model, arguments.Enabled)
	case "delete":
		return self.executeDelete(userId, arguments.ID)
	case "trigger":
		return self.executeTrigger(userId, arguments.ID)
	default:
		return "", fmt.Errorf("unknown jobs action: %s", arguments.Action)
	}
}

func (self *jobsTool) executeList(userId string) (string, error) {
	jobs := self.scheduler.List(userId)
	result, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"jobs":   jobs,
	})
	return string(result), nil
}

func (self *jobsTool) executeCreate(ctx context.Context, userId string, name string, schedule string, message string, model string, agentId string, delay string, oneShot *bool) (string, error) {
	if name == "" || message == "" {
		return "", fmt.Errorf("name and message are required")
	}
	if delay != "" && schedule != "" {
		return "", fmt.Errorf("provide either 'schedule' or 'delay', not both")
	}
	if delay == "" && schedule == "" {
		return "", fmt.Errorf("either 'schedule' or 'delay' is required")
	}

	var runAt int64
	isOneShot := false
	conversationId := "" // recurring jobs resolve conversation at execution time

	if delay != "" {
		duration, parseError := time.ParseDuration(delay)
		if parseError != nil {
			return "", fmt.Errorf("invalid delay %q: %w (use Go duration format: '30m', '1h', '2h30m')", delay, parseError)
		}
		if duration < time.Minute {
			return "", fmt.Errorf("delay must be at least 1 minute, got %s", duration)
		}
		runAt = time.Now().Add(duration).UnixMilli()
		isOneShot = true
		// One-shot reminders bind to the current conversation.
		if contextConversationId := agents.ConversationIDFromContext(ctx); contextConversationId != "" {
			conversationId = contextConversationId
		} else {
			conversationId = security.NewULID()
		}
	} else {
		if _, parseError := cronexpr.Parse(schedule); parseError != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", parseError)
		}
	}

	if oneShot != nil {
		isOneShot = *oneShot
	}

	job := Job{
		ID:             security.NewULID(),
		Name:           name,
		Schedule:       schedule,
		Message:        message,
		Model:          model,
		AgentID:        agentId,
		Enabled:        true,
		ConversationID: conversationId,
		RunAt:          runAt,
		OneShot:        isOneShot,
		CreatedAt:      time.Now().UnixMilli(),
	}

	if err := self.scheduler.CreateAndReload(userId, job); err != nil {
		return "", fmt.Errorf("creating job: %w", err)
	}

	response := map[string]interface{}{
		"action":  "create",
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

func (self *jobsTool) executeUpdate(userId, id string, name string, schedule string, message string, model string, enabled *bool) (string, error) {
	if id == "" {
		return "", fmt.Errorf("id is required")
	}

	// Find existing job.
	jobs := self.scheduler.List(userId)
	var job *Job
	for index := range jobs {
		if jobs[index].ID == id {
			job = &jobs[index]
			break
		}
	}
	if job == nil {
		return "", fmt.Errorf("job not found: %s", id)
	}

	if name != "" {
		job.Name = name
	}
	if schedule != "" {
		if _, err := cronexpr.Parse(schedule); err != nil {
			return "", fmt.Errorf("invalid schedule expression: %w", err)
		}
		job.Schedule = schedule
	}
	if message != "" {
		job.Message = message
	}
	if model != "" {
		job.Model = model
	}
	if enabled != nil {
		job.Enabled = *enabled
	}

	if err := self.scheduler.UpdateAndReload(userId, *job); err != nil {
		return "", fmt.Errorf("updating job: %w", err)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "update",
		"id":      job.ID,
		"name":    job.Name,
		"success": true,
	})
	return string(result), nil
}

func (self *jobsTool) executeDelete(userId, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("id is required")
	}

	if err := self.scheduler.DeleteAndReload(userId, id); err != nil {
		return "", fmt.Errorf("deleting job: %w", err)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "delete",
		"id":      id,
		"success": true,
	})
	return string(result), nil
}

func (self *jobsTool) executeTrigger(userId, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("id is required")
	}

	if err := self.scheduler.Trigger(userId, id); err != nil {
		return "", err
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "trigger",
		"id":      id,
		"success": true,
	})
	return string(result), nil
}

// truncateString truncates a string to maxLength, appending "..." if truncated.
func truncateString(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + "..."
}
