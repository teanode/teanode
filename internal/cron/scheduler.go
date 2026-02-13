package cron

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.Get("cron")

// Scheduler runs cron jobs on a 1-minute tick.
type Scheduler struct {
	store  *Store
	runner *agent.Runner
	mutex  sync.Mutex
	jobs   []CronJob
	expressions  map[string]*CronExpr
	stopChannel chan struct{}

	Broadcast      func(event string, payload interface{})
	SetActiveRun   func(sessionKey, runId string)
	ClearActiveRun func(sessionKey, runId string)
}

// NewScheduler creates a new cron scheduler.
func NewScheduler(store *Store, runner *agent.Runner) *Scheduler {
	return &Scheduler{
		store:  store,
		runner: runner,
		expressions:  make(map[string]*CronExpr),
		stopChannel: make(chan struct{}),
	}
}

// Start loads jobs and begins the ticker goroutine.
func (self *Scheduler) Start() error {
	if err := self.Reload(); err != nil {
		return fmt.Errorf("loading cron jobs: %w", err)
	}
	go self.run()
	log.Infof("cron scheduler started with %d jobs", len(self.jobs))
	return nil
}

// Stop halts the scheduler.
func (self *Scheduler) Stop() {
	close(self.stopChannel)
}

// Reload re-reads jobs from disk and rebuilds the expression cache.
func (self *Scheduler) Reload() error {
	jobs, err := self.store.Load()
	if err != nil {
		return err
	}

	expressions := make(map[string]*CronExpr)
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		expr, err := Parse(job.Schedule)
		if err != nil {
			log.Errorf("bad cron expression for job %s (%s): %v", job.ID, job.Schedule, err)
			continue
		}
		expressions[job.ID] = expr
	}

	self.mutex.Lock()
	self.jobs = jobs
	self.expressions = expressions
	self.mutex.Unlock()
	return nil
}

// List returns the current in-memory job list.
func (self *Scheduler) List() []CronJob {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make([]CronJob, len(self.jobs))
	copy(result, self.jobs)
	return result
}

// Trigger manually runs a job immediately.
func (self *Scheduler) Trigger(id string) error {
	self.mutex.Lock()
	var job *CronJob
	for index := range self.jobs {
		if self.jobs[index].ID == id {
			job = &self.jobs[index]
			break
		}
	}
	self.mutex.Unlock()

	if job == nil {
		return fmt.Errorf("job not found: %s", id)
	}

	go self.executeJob(*job)
	return nil
}

// CreateAndReload creates a job in the store and reloads the scheduler.
func (self *Scheduler) CreateAndReload(job CronJob) error {
	if err := self.store.Create(job); err != nil {
		return err
	}
	return self.Reload()
}

// UpdateAndReload updates a job in the store and reloads the scheduler.
func (self *Scheduler) UpdateAndReload(job CronJob) error {
	if err := self.store.Update(job); err != nil {
		return err
	}
	return self.Reload()
}

// DeleteAndReload deletes a job from the store and reloads the scheduler.
func (self *Scheduler) DeleteAndReload(id string) error {
	if err := self.store.Delete(id); err != nil {
		return err
	}
	return self.Reload()
}

func (self *Scheduler) run() {
	defer deferutil.Recover()

	// Align to next minute boundary.
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	for {
		select {
		case <-self.stopChannel:
			return
		case tickTime := <-timer.C:
			self.tick(tickTime)
			// Reset timer for next minute.
			next = tickTime.Truncate(time.Minute).Add(time.Minute)
			timer.Reset(time.Until(next))
		}
	}
}

func (self *Scheduler) tick(when time.Time) {
	self.mutex.Lock()
	jobs := make([]CronJob, len(self.jobs))
	copy(jobs, self.jobs)
	expressions := self.expressions
	self.mutex.Unlock()

	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		expr, ok := expressions[job.ID]
		if !ok {
			continue
		}
		if expr.Matches(when) {
			go self.executeJob(job)
		}
	}
}

func (self *Scheduler) executeJob(job CronJob) {
	defer deferutil.Recover()

	runId := uuid.New().String()
	log.Infof("executing cron job %s (%s) -> session %s run %s", job.ID, job.Name, job.SessionKey, runId)

	if self.SetActiveRun != nil {
		self.SetActiveRun(job.SessionKey, runId)
	}
	defer func() {
		if self.ClearActiveRun != nil {
			self.ClearActiveRun(job.SessionKey, runId)
		}
	}()

	// Broadcast user message to WebUI.
	if self.Broadcast != nil {
		self.Broadcast("chat", map[string]interface{}{
			"state":      "user_message",
			"runId":      runId,
			"sessionKey": job.SessionKey,
			"text":       job.Message,
		})
	}

	// Set a human-readable session title.
	title := fmt.Sprintf("Cron: %s", job.Name)
	self.runner.Sessions.SetTitle(job.SessionKey, title)
	if self.Broadcast != nil {
		self.Broadcast("sessions", nil)
	}

	var callbacks *agent.RunCallbacks
	if self.Broadcast != nil {
		broadcast := self.Broadcast
		callbacks = &agent.RunCallbacks{
			OnTextDelta: func(text string) {
				broadcast("chat", map[string]interface{}{
					"state":      "delta",
					"runId":      runId,
					"sessionKey": job.SessionKey,
					"text":       text,
				})
			},
			OnToolCall: func(toolName string, arguments string) {
				broadcast("chat", map[string]interface{}{
					"state":      "tool_call",
					"runId":      runId,
					"sessionKey": job.SessionKey,
					"toolName":   toolName,
					"arguments":  arguments,
				})
			},
			OnToolResult: func(toolName string, result string) {
				broadcast("chat", map[string]interface{}{
					"state":      "tool_result",
					"runId":      runId,
					"sessionKey": job.SessionKey,
					"toolName":   toolName,
					"result":     result,
				})
			},
			OnTitleUpdate: func(title string) {
				broadcast("chat", map[string]interface{}{
					"state":      "title",
					"sessionKey": job.SessionKey,
					"title":      title,
				})
				broadcast("sessions", nil)
			},
		}
	}

	result, err := self.runner.Run(context.Background(), agent.RunParams{
		SessionKey: job.SessionKey,
		Message:    job.Message,
		Model:      job.Model,
	}, callbacks)

	if self.Broadcast != nil {
		if err != nil {
			self.Broadcast("chat", map[string]interface{}{
				"state":      "error",
				"runId":      runId,
				"sessionKey": job.SessionKey,
				"error":      err.Error(),
			})
		} else {
			payload := map[string]interface{}{
				"state":      "final",
				"runId":      runId,
				"sessionKey": job.SessionKey,
				"text":       result.Response,
				"model":      result.Model,
				"stopReason": result.StopReason,
			}
			if result.Usage != nil {
				payload["usage"] = result.Usage
			}
			self.Broadcast("chat", payload)
		}
	}

	now := time.Now().UnixMilli()
	if err != nil {
		log.Errorf("cron job %s failed: %v", job.ID, err)
		job.LastRun = now
		job.LastStatus = "error"
		job.LastError = err.Error()
	} else {
		job.LastRun = now
		job.LastStatus = "success"
		job.LastError = ""
	}

	if err := self.store.Update(job); err != nil {
		log.Errorf("updating cron job status: %v", err)
	}

	// Update in-memory state.
	self.mutex.Lock()
	for index := range self.jobs {
		if self.jobs[index].ID == job.ID {
			self.jobs[index].LastRun = job.LastRun
			self.jobs[index].LastStatus = job.LastStatus
			self.jobs[index].LastError = job.LastError
			break
		}
	}
	self.mutex.Unlock()
}
