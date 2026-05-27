// Package jobs exposes tools for inspecting and managing scheduled jobs.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	jobscore "github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&jobsTool{}}
	})
}

// jobsTool supports list/create/update/delete/trigger actions.
type jobsTool struct{}

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
					"trigger": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"scheduled", "webhook"},
						"description": "Job trigger type. Defaults to 'scheduled'. Webhook jobs ignore schedule and delay.",
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

func (self *jobsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *jobsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action            string `json:"action"`
		ID                string `json:"id"`
		Name              string `json:"name"`
		Schedule          string `json:"schedule"`
		Message           string `json:"message"`
		Trigger           string `json:"trigger"`
		ProviderModelName string `json:"model"`
		AgentID           string `json:"agentId"`
		Delay             string `json:"delay"`
		OneShot           *bool  `json:"oneShot"`
		Enabled           *bool  `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("jobs: parsing arguments: %w", err)
	}

	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("jobs: authenticated user context is required")
	}
	userId := user.ID

	switch arguments.Action {
	case "list":
		return self.executeList(ctx, userId)
	case "create":
		return self.executeCreate(ctx, userId, arguments.Name, arguments.Trigger, arguments.Schedule, arguments.Message, arguments.ProviderModelName, arguments.AgentID, arguments.Delay, arguments.OneShot)
	case "update":
		return self.executeUpdate(ctx, userId, arguments.ID, arguments.Name, arguments.Schedule, arguments.Message, arguments.ProviderModelName, arguments.Enabled)
	case "delete":
		return self.executeDelete(ctx, userId, arguments.ID)
	case "trigger":
		return self.executeTrigger(ctx, userId, arguments.ID)
	default:
		return "", fmt.Errorf("jobs: unknown jobs action: %s", arguments.Action)
	}
}

func (self *jobsTool) executeList(ctx context.Context, userId string) (string, error) {
	jobModels := make([]*models.Job, 0)
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedJobs, listError := transaction.ListJobs(ctx, userId, nil)
		if listError != nil {
			return listError
		}
		jobModels = listedJobs
		return nil
	}); err != nil {
		return "", err
	}
	result, _ := json.Marshal(map[string]interface{}{
		"action": "list",
		"jobs":   sanitizeJobsForTool(jobModels),
	})
	return string(result), nil
}

func (self *jobsTool) executeCreate(ctx context.Context, userId string, name string, trigger string, schedule string, message string, providerModelName string, agentId string, delay string, oneShot *bool) (string, error) {
	if name == "" || message == "" {
		return "", fmt.Errorf("jobs: name and message are required")
	}
	triggerKind := models.JobTriggerKindScheduled
	if trigger != "" {
		triggerKind = models.JobTriggerKind(trigger)
	}
	if triggerKind != models.JobTriggerKindScheduled && triggerKind != models.JobTriggerKindWebhook {
		return "", fmt.Errorf("jobs: unsupported trigger %q", trigger)
	}
	if delay != "" && schedule != "" {
		return "", fmt.Errorf("jobs: provide either 'schedule' or 'delay', not both")
	}
	if triggerKind == models.JobTriggerKindScheduled && delay == "" && schedule == "" {
		return "", fmt.Errorf("jobs: either 'schedule' or 'delay' is required")
	}

	isOneShot := false
	conversationId := "" // recurring jobs resolve conversation at execution time
	var runAt *time.Time
	webhookSecret := ""

	if triggerKind == models.JobTriggerKindWebhook {
		webhookSecret = security.GenerateRandomString(32, security.LowerAlphaNumeric)
	} else if delay != "" {
		duration, parseError := time.ParseDuration(delay)
		if parseError != nil {
			return "", fmt.Errorf("jobs: invalid delay %q: %w (use Go duration format: '30m', '1h', '2h30m')", delay, parseError)
		}
		if duration < time.Minute {
			return "", fmt.Errorf("jobs: delay must be at least 1 minute, got %s", duration)
		}
		runAtValue := time.Now().Add(duration)
		runAt = &runAtValue
		isOneShot = true
		// One-shot reminders bind to the current conversation.
		conversationId = runners.RunnerFromContext(ctx).ConversationID
	} else {
		if _, parseError := cronexpr.Parse(schedule); parseError != nil {
			return "", fmt.Errorf("jobs: invalid schedule expression: %w", parseError)
		}
	}

	if oneShot != nil {
		isOneShot = *oneShot
	}

	job := models.Job{
		Name:              ptrto.Value(name),
		Trigger:           ptrto.Value(triggerKind),
		Schedule:          ptrto.TrimmedString(schedule),
		WebhookSecret:     ptrto.TrimmedString(webhookSecret),
		Prompt:            ptrto.Value(message),
		ProviderModelName: ptrto.TrimmedString(providerModelName),
		AgentID:           ptrto.TrimmedString(agentId),
		Enabled:           ptrto.Value(true),
		ConversationID:    ptrto.TrimmedString(conversationId),
		RunAt:             runAt,
		OneShot:           ptrto.Value(isOneShot),
	}
	job.UserID = ptrto.Value(userId)

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		createdJob, createError := transaction.CreateJob(ctx, &job, nil)
		if createError == nil && createdJob != nil {
			job = *createdJob
		}
		return createError
	}); err != nil {
		return "", fmt.Errorf("jobs: creating job: %w", err)
	}

	response := map[string]interface{}{
		"action":  "create",
		"id":      job.ID,
		"name":    job.GetName(),
		"agentId": job.GetAgentID(),
		"trigger": job.GetTrigger(),
	}
	if job.GetSchedule() != "" {
		response["schedule"] = job.GetSchedule()
	}
	if job.RunAt != nil {
		response["firesAt"] = job.RunAt.Format(time.RFC3339)
	}
	if job.GetTrigger() == models.JobTriggerKindWebhook {
		response["webhookPath"] = webhookPath(job.ID)
		response["webhookSecret"] = job.GetWebhookSecret()
	}
	result, _ := json.Marshal(response)
	return string(result), nil
}

func (self *jobsTool) executeUpdate(ctx context.Context, userId, id string, name string, schedule string, message string, providerModelName string, enabled *bool) (string, error) {
	if id == "" {
		return "", fmt.Errorf("jobs: id is required")
	}

	var job *models.Job
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, id, nil)
		if getError != nil {
			return getError
		}
		if existingJob.GetUserID() != userId {
			return store.ErrNotFound
		}
		job = existingJob
		return nil
	}); err != nil {
		return "", fmt.Errorf("jobs: job not found: %s", id)
	}

	if name != "" {
		job.Name = ptrto.Value(name)
	}
	if schedule != "" {
		if _, err := cronexpr.Parse(schedule); err != nil {
			return "", fmt.Errorf("jobs: invalid schedule expression: %w", err)
		}
		job.Schedule = ptrto.Value(schedule)
	}
	if message != "" {
		job.Prompt = ptrto.Value(message)
	}
	if providerModelName != "" {
		job.ProviderModelName = ptrto.Value(providerModelName)
	}
	if enabled != nil {
		job.Enabled = ptrto.Value(*enabled)
	}

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		job.UserID = ptrto.Value(userId)
		_, modifyError := transaction.ModifyJob(ctx, job.ID, func(existingJob *models.Job) error {
			*existingJob = *job
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		return "", fmt.Errorf("jobs: updating job: %w", err)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "update",
		"id":      job.ID,
		"name":    job.GetName(),
		"success": true,
	})
	return string(result), nil
}

func (self *jobsTool) executeDelete(ctx context.Context, userId, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("jobs: id is required")
	}

	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, id, nil)
		if getError != nil {
			return getError
		}
		if existingJob.GetUserID() != userId {
			return store.ErrNotFound
		}
		return transaction.DeleteJob(ctx, id, nil)
	}); err != nil {
		return "", fmt.Errorf("jobs: deleting job: %w", err)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "delete",
		"id":      id,
		"success": true,
	})
	return string(result), nil
}

func (self *jobsTool) executeTrigger(ctx context.Context, userId string, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("jobs: id is required")
	}

	// Verify ownership before triggering.
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, id, nil)
		if getError != nil {
			return getError
		}
		if existingJob.GetUserID() != userId {
			return store.ErrNotFound
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("jobs: job not found: %s", id)
	}

	if err := jobscore.SchedulerFromContext(ctx).TriggerJob(ctx, id); err != nil {
		return "", err
	}

	result, _ := json.Marshal(map[string]interface{}{
		"action":  "trigger",
		"id":      id,
		"trigger": models.JobTriggerKindManual,
		"success": true,
	})
	return string(result), nil
}

func sanitizeJobsForTool(jobModels []*models.Job) []*models.Job {
	sanitizedJobs := make([]*models.Job, 0, len(jobModels))
	for _, jobModel := range jobModels {
		if jobModel == nil {
			continue
		}
		jobCopy := *jobModel
		jobCopy.WebhookSecret = nil
		sanitizedJobs = append(sanitizedJobs, &jobCopy)
	}
	return sanitizedJobs
}

func webhookPath(jobId string) string {
	return "/api/jobs/" + jobId + "/webhook"
}
