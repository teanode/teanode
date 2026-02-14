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
			Description: "List all cron jobs with their schedule, status, and last run info.",
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
			Name:        "cron_create",
			Description: "Create a new scheduled cron job.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name for the cron job.",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron expression (5-field: minute hour day-of-month month day-of-week). Example: '0 9 * * 1-5' for 9am weekdays.",
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
				},
				"required": []string{"name", "schedule", "message"},
			},
		},
	}
}

func (self *cronCreateTool) Execute(_ context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Message  string `json:"message"`
		Model    string `json:"model"`
		AgentID  string `json:"agentId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Name == "" || arguments.Schedule == "" || arguments.Message == "" {
		return "", fmt.Errorf("name, schedule, and message are required")
	}

	if _, err := Parse(arguments.Schedule); err != nil {
		return "", fmt.Errorf("invalid cron expression: %w", err)
	}

	job := CronJob{
		ID:         uuid.New().String()[:8],
		Name:       arguments.Name,
		Schedule:   arguments.Schedule,
		Message:    arguments.Message,
		Model:      arguments.Model,
		AgentID:    arguments.AgentID,
		Enabled:    true,
		SessionKey: GenerateSessionKey(arguments.Name),
		CreatedAt:  time.Now().UnixMilli(),
	}

	if err := self.scheduler.store.Create(job); err != nil {
		return "", fmt.Errorf("creating job: %w", err)
	}
	if err := self.scheduler.Reload(); err != nil {
		return "", fmt.Errorf("reloading scheduler: %w", err)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"id":       job.ID,
		"name":     job.Name,
		"schedule": job.Schedule,
		"agentId":  job.AgentID,
	})
	return string(result), nil
}

// --- cron_update ---

type cronUpdateTool struct{ scheduler *Scheduler }

func (self *cronUpdateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "cron_update",
			Description: "Update an existing cron job.",
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
			Description: "Delete a cron job.",
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
