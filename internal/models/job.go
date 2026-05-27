package models

import "time"

type JobStatus string

const (
	JobStatusSuccess JobStatus = "success"
	JobStatusError   JobStatus = "error"
)

type JobTriggerKind string

const (
	JobTriggerKindManual    JobTriggerKind = "manual"
	JobTriggerKindScheduled JobTriggerKind = "scheduled"
	JobTriggerKindWebhook   JobTriggerKind = "webhook"
)

type JobRunStatus string

const (
	JobRunStatusRunning JobRunStatus = "running"
	JobRunStatusSuccess JobRunStatus = "success"
	JobRunStatusError   JobRunStatus = "error"
)

type Job struct {
	ID                string          `json:"id,omitempty" yaml:"id,omitempty"`
	UserID            *string         `json:"userId,omitempty" yaml:"userId,omitempty"`
	ProviderModelName *string         `json:"providerModelName,omitempty" yaml:"model,omitempty"`
	AgentID           *string         `json:"agentId,omitempty" yaml:"agentId,omitempty"`
	ConversationID    *string         `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	Name              *string         `json:"name,omitempty" yaml:"name,omitempty"`
	Trigger           *JobTriggerKind `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Schedule          *string         `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	WebhookSecret     *string         `json:"webhookSecret,omitempty" yaml:"webhookSecret,omitempty"`
	Prompt            *string         `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Enabled           *bool           `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	OneShot           *bool           `json:"oneShot,omitempty" yaml:"oneShot,omitempty"`
	LastStatus        *JobStatus      `json:"lastStatus,omitempty" yaml:"lastStatus,omitempty"`
	LastError         *string         `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	RunAt             *time.Time      `json:"runAt,omitempty" yaml:"runAt,omitempty"`
	LastRunAt         *time.Time      `json:"lastRunAt,omitempty" yaml:"lastRunAt,omitempty"`
	CreatedAt         *time.Time      `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt        *time.Time      `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}

type JobRun struct {
	ID                   string          `json:"id,omitempty" yaml:"id,omitempty"`
	JobID                *string         `json:"jobId,omitempty" yaml:"jobId,omitempty"`
	UserID               *string         `json:"userId,omitempty" yaml:"userId,omitempty"`
	Trigger              *JobTriggerKind `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Status               *JobRunStatus   `json:"status,omitempty" yaml:"status,omitempty"`
	RunID                *string         `json:"runId,omitempty" yaml:"runId,omitempty"`
	Error                *string         `json:"error,omitempty" yaml:"error,omitempty"`
	StartedAt            *time.Time      `json:"startedAt,omitempty" yaml:"startedAt,omitempty"`
	CompletedAt          *time.Time      `json:"completedAt,omitempty" yaml:"completedAt,omitempty"`
	DurationMilliseconds *int64          `json:"durationMilliseconds,omitempty" yaml:"durationMilliseconds,omitempty"`
	RequestMethod        *string         `json:"requestMethod,omitempty" yaml:"requestMethod,omitempty"`
	RequestPath          *string         `json:"requestPath,omitempty" yaml:"requestPath,omitempty"`
	RemoteAddress        *string         `json:"remoteAddress,omitempty" yaml:"remoteAddress,omitempty"`
}
