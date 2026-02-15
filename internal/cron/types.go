package cron

import (
	"github.com/google/uuid"
)

// GenerateSessionKey creates a unique session key for a cron job.
func GenerateSessionKey() string {
	return uuid.New().String()
}

// CronJob represents a scheduled job.
type CronJob struct {
	ID         string `json:"id" yaml:"id"`
	Name       string `json:"name" yaml:"name"`
	Schedule   string `json:"schedule" yaml:"schedule"` // 5-field cron expr
	Message    string `json:"message" yaml:"message"`
	Model      string `json:"model,omitempty" yaml:"model,omitempty"`
	AgentID    string `json:"agentId,omitempty" yaml:"agentId,omitempty"` // target agent; defaults to "main"
	Enabled    bool   `json:"enabled" yaml:"enabled"`
	SessionKey string `json:"sessionKey" yaml:"sessionKey"`                         // persistent session for this job
	LastRun    int64  `json:"lastRun,omitempty" yaml:"lastRun,omitempty"`           // unix ms
	LastStatus string `json:"lastStatus,omitempty" yaml:"lastStatus,omitempty"`     // "success" | "error"
	LastError  string `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	CreatedAt  int64  `json:"createdAt" yaml:"createdAt"`
}

type cronFile struct {
	Version int       `json:"version" yaml:"version"`
	Jobs    []CronJob `json:"jobs" yaml:"jobs"`
}
