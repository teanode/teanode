package api

import (
	"context"

	"github.com/teanode/teanode/internal/jobs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

// handleJobsList: list all jobs.
func (self *webSocketConnection) handleJobsList(frame requestFrame) (interface{}, error) {
	jobsList := make([]*models.Job, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedJobs, listError := transaction.ListJobs(ctx, self.userId(), nil)
		if listError != nil {
			return listError
		}
		jobsList = listedJobs
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing jobs: "+err.Error())
	}
	return map[string]interface{}{
		"jobs": sanitizeJobs(jobsList),
	}, nil
}

// jobCreateParameters are the parameters for job.create.
type jobCreateParameters struct {
	Job models.Job `json:"job"`
}

// handleJobsCreate: create a new job.
func (self *webSocketConnection) handleJobsCreate(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[jobCreateParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.Job.GetName() == "" {
		return nil, rpcError(400, "job.name is required")
	}
	if parameters.Job.GetTrigger() == models.JobTriggerKindWebhook {
		parameters.Job.Schedule = nil
		parameters.Job.RunAt = nil
		parameters.Job.OneShot = nil
		if parameters.Job.GetWebhookSecret() == "" {
			parameters.Job.WebhookSecret = ptrto.Value(generateWebhookSecret())
		}
	} else if parameters.Job.GetSchedule() == "" && parameters.Job.RunAt == nil {
		return nil, rpcError(400, "job.schedule or job.runAt is required")
	}
	if parameters.Job.GetConversationID() == "" {
		defaultConversationId := self.api.coordinator.EnsureDefaultConversation(self.userId(), parameters.Job.GetAgentID())
		parameters.Job.ConversationID = ptrto.Value(defaultConversationId)
	}
	parameters.Job.UserID = ptrto.Value(self.userId())
	var createdJob *models.Job
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		var createError error
		createdJob, createError = transaction.CreateJob(ctx, &parameters.Job, nil)
		return createError
	}); err != nil {
		return nil, rpcError(500, "creating job: "+err.Error())
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeJobs, nil)
	return map[string]interface{}{
		"job": createdJob,
	}, nil
}

// jobUpdateParameters are the parameters for job.update.
type jobUpdateParameters struct {
	Job models.Job `json:"job"`
}

// handleJobsUpdate: update an existing job.
func (self *webSocketConnection) handleJobsUpdate(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[jobUpdateParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.Job.ID == "" {
		return nil, rpcError(400, "job.id is required")
	}
	var updatedJob *models.Job
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, parameters.Job.ID, nil)
		if getError != nil {
			return getError
		}
		if existingJob.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		var modifyError error
		updatedJob, modifyError = transaction.ModifyJob(ctx, parameters.Job.ID, func(job *models.Job) error {
			mergeJobUpdate(job, &parameters.Job)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		return nil, rpcError(500, "updating job: "+err.Error())
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeJobs, nil)
	return map[string]interface{}{
		"job": updatedJob,
	}, nil
}

// mergeJobUpdate applies non-nil fields from patch onto an existing job.
func mergeJobUpdate(job *models.Job, patch *models.Job) {
	if patch.Name != nil {
		job.Name = patch.Name
	}
	if patch.Trigger != nil {
		job.Trigger = patch.Trigger
	}
	if patch.Schedule != nil {
		job.Schedule = patch.Schedule
	}
	if patch.WebhookSecret != nil {
		job.WebhookSecret = patch.WebhookSecret
	}
	if patch.RunAt != nil {
		job.RunAt = patch.RunAt
	}
	if patch.Prompt != nil {
		job.Prompt = patch.Prompt
	}
	if patch.ProviderModelName != nil {
		job.ProviderModelName = patch.ProviderModelName
	}
	if patch.AgentID != nil {
		job.AgentID = patch.AgentID
	}
	if patch.ConversationID != nil {
		job.ConversationID = patch.ConversationID
	}
	if patch.Enabled != nil {
		job.Enabled = patch.Enabled
	}
	if patch.OneShot != nil {
		job.OneShot = patch.OneShot
	}
}

// jobDeleteParameters are the parameters for job.delete.
type jobDeleteParameters struct {
	ID string `json:"id"`
}

// handleJobsDelete: delete a job.
func (self *webSocketConnection) handleJobsDelete(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[jobDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.ID == "" {
		return nil, rpcError(400, "id is required")
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		job, getError := transaction.GetJob(ctx, parameters.ID, nil)
		if getError != nil {
			return getError
		}
		if job.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return transaction.DeleteJob(ctx, parameters.ID, nil)
	}); err != nil {
		return nil, rpcError(500, "deleting job: "+err.Error())
	}

	return map[string]interface{}{
		"deleted": true,
	}, nil
}

// jobTriggerParameters are the parameters for job.trigger.
type jobTriggerParameters struct {
	ID string `json:"id"`
}

// handleJobsTrigger: manually trigger a job.
func (self *webSocketConnection) handleJobsTrigger(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[jobTriggerParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.ID == "" {
		return nil, rpcError(400, "id is required")
	}

	// Verify the requesting user owns this job.
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		job, getError := transaction.GetJob(ctx, parameters.ID, nil)
		if getError != nil {
			return getError
		}
		if job.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		return nil
	}); err != nil {
		return nil, rpcError(404, "job not found")
	}

	if err := jobs.SchedulerFromContext(self.ctx).TriggerJob(self.ctx, parameters.ID); err != nil {
		return nil, rpcError(500, "triggering job: "+err.Error())
	}

	return map[string]interface{}{
		"triggered": true,
	}, nil
}

type jobRunsListParameters struct {
	JobID string `json:"jobId"`
}

func (self *webSocketConnection) handleJobRunsList(frame requestFrame) (interface{}, error) {
	parameters, err := unmarshalParameters[jobRunsListParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.JobID == "" {
		return nil, rpcError(400, "jobId is required")
	}

	jobRuns := make([]*models.JobRun, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		job, getError := transaction.GetJob(ctx, parameters.JobID, nil)
		if getError != nil && getError != store.ErrNotFound {
			return getError
		}
		if job != nil && job.GetUserID() != self.userId() {
			return store.ErrNotFound
		}
		listedJobRuns, listError := transaction.ListJobRuns(ctx, parameters.JobID, nil)
		if listError != nil {
			return listError
		}
		for _, jobRun := range listedJobRuns {
			if jobRun.GetUserID() != self.userId() {
				continue
			}
			jobRuns = append(jobRuns, jobRun)
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing job runs: "+err.Error())
	}

	return map[string]interface{}{
		"jobRuns": jobRuns,
	}, nil
}

func sanitizeJobs(jobModels []*models.Job) []*models.Job {
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
