package cron

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
	RunAt      int64  `json:"runAt,omitempty" yaml:"runAt,omitempty"`               // unix ms; one-shot fire time
	OneShot    bool   `json:"oneShot,omitempty" yaml:"oneShot,omitempty"`           // auto-delete after execution
	LastRun    int64  `json:"lastRun,omitempty" yaml:"lastRun,omitempty"`           // unix ms
	LastStatus string `json:"lastStatus,omitempty" yaml:"lastStatus,omitempty"`     // "success" | "error"
	LastError  string `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	CreatedAt  int64  `json:"createdAt" yaml:"createdAt"`
}

type cronFile struct {
	Version int       `json:"version" yaml:"version"`
	Jobs    []CronJob `json:"jobs" yaml:"jobs"`
}
