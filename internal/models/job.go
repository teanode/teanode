package models

import "time"

type JobStatus string

const (
	JobStatusSuccess JobStatus = "success"
	JobStatusError   JobStatus = "error"
)

type Job struct {
	ID                string     `json:"id,omitempty" yaml:"id,omitempty"`
	UserID            *string    `json:"userId,omitempty" yaml:"userId,omitempty"`
	ProviderModelName *string    `json:"providerModelName,omitempty" yaml:"model,omitempty"`
	AgentID           *string    `json:"agentId,omitempty" yaml:"agentId,omitempty"`
	ConversationID    *string    `json:"conversationId,omitempty" yaml:"conversationId,omitempty"`
	Name              *string    `json:"name,omitempty" yaml:"name,omitempty"`
	Schedule          *string    `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	Prompt            *string    `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Enabled           *bool      `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	OneShot           *bool      `json:"oneShot,omitempty" yaml:"oneShot,omitempty"`
	LastStatus        *JobStatus `json:"lastStatus,omitempty" yaml:"lastStatus,omitempty"`
	LastError         *string    `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	RunAt             *time.Time `json:"runAt,omitempty" yaml:"runAt,omitempty"`
	LastRunAt         *time.Time `json:"lastRunAt,omitempty" yaml:"lastRunAt,omitempty"`
	CreatedAt         *time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt        *time.Time `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}
