package cron

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GenerateSessionKey creates a stable session key for a cron job based on its name.
func GenerateSessionKey(name string) string {
	safeName := strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			return char
		}
		return '-'
	}, name)
	return fmt.Sprintf("cron-%s-%s", safeName, uuid.New().String()[:8])
}

// CronJob represents a scheduled job.
type CronJob struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Schedule   string `json:"schedule"`            // 5-field cron expr
	Message    string `json:"message"`
	Model      string `json:"model,omitempty"`
	Enabled    bool   `json:"enabled"`
	SessionKey string `json:"sessionKey"`           // persistent session for this job
	LastRun    int64  `json:"lastRun,omitempty"`    // unix ms
	LastStatus string `json:"lastStatus,omitempty"` // "success" | "error"
	LastError  string `json:"lastError,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
}

type cronFile struct {
	Version int       `json:"version"`
	Jobs    []CronJob `json:"jobs"`
}
