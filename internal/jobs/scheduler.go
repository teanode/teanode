package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/ulid"
)

// Scheduler runs scheduled jobs on a 1-minute tick.
type Scheduler struct {
	store         *Store
	agentRegistry *agents.AgentRegistry
	mutex         sync.Mutex
	jobs          []Job
	expressions   map[string]*cronexpr.CronExpr
	stopChannel   chan struct{}

	Broadcast      func(event string, payload interface{})
	SetActiveRun   func(conversationId, runId string)
	ClearActiveRun func(conversationId, runId string)
}

// NewScheduler creates a new job scheduler.
func NewScheduler(store *Store, agentRegistry *agents.AgentRegistry) *Scheduler {
	return &Scheduler{
		store:         store,
		agentRegistry: agentRegistry,
		expressions:   make(map[string]*cronexpr.CronExpr),
		stopChannel:   make(chan struct{}),
	}
}

// Start loads jobs and begins the ticker goroutine.
func (self *Scheduler) Start() error {
	if err := self.Reload(); err != nil {
		return fmt.Errorf("loading jobs: %w", err)
	}
	go self.run()
	log.Infof("job scheduler started with %d jobs", len(self.jobs))
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

	expressions := make(map[string]*cronexpr.CronExpr)
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		if job.RunAt > 0 {
			continue // one-shot timer jobs don't use schedule expressions
		}
		expr, err := cronexpr.Parse(job.Schedule)
		if err != nil {
			log.Errorf("bad schedule expression for job %s (%s): %v", job.ID, job.Schedule, err)
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
func (self *Scheduler) List() []Job {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make([]Job, len(self.jobs))
	copy(result, self.jobs)
	return result
}

// Trigger manually runs a job immediately.
func (self *Scheduler) Trigger(id string) error {
	self.mutex.Lock()
	var job *Job
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
func (self *Scheduler) CreateAndReload(job Job) error {
	if err := self.store.Create(job); err != nil {
		return err
	}
	return self.Reload()
}

// UpdateAndReload updates a job in the store and reloads the scheduler.
func (self *Scheduler) UpdateAndReload(job Job) error {
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
	jobs := make([]Job, len(self.jobs))
	copy(jobs, self.jobs)
	expressions := self.expressions
	self.mutex.Unlock()

	nowMilliseconds := when.UnixMilli()
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		if job.RunAt > 0 {
			if nowMilliseconds >= job.RunAt {
				go self.executeJob(job)
			}
			continue
		}
		expression, ok := expressions[job.ID]
		if !ok {
			continue
		}
		if expression.Matches(when) {
			go self.executeJob(job)
		}
	}
}

func (self *Scheduler) executeJob(job Job) {
	defer deferutil.Recover()

	// Immediately disable one-shot jobs to prevent duplicate execution on the next tick.
	if job.OneShot {
		job.Enabled = false
		_ = self.store.Update(job)
		self.mutex.Lock()
		for index := range self.jobs {
			if self.jobs[index].ID == job.ID {
				self.jobs[index].Enabled = false
				break
			}
		}
		self.mutex.Unlock()
	}

	// Resolve the runner for this job's agent.
	agentId := job.AgentID
	if agentId == "" {
		agentId = self.agentRegistry.ActiveAgentID()
	}
	runner := self.agentRegistry.Get(agentId)
	if runner == nil {
		runner = self.agentRegistry.Default()
	}
	if runner == nil {
		log.Errorf("job %s: no runner available for agent %q", job.ID, agentId)
		return
	}

	// Resolve conversation: use stored value if present (backward compat), otherwise use active conversation.
	conversationId := job.ConversationID
	if conversationId == "" {
		conversationId = self.agentRegistry.ActiveConversationID(agentId)
	}

	runId := ulid.GenerateString()
	log.Infof("executing job %s (%s) -> agent %s conversation %s run %s", job.ID, job.Name, agentId, conversationId, runId)

	if self.SetActiveRun != nil {
		self.SetActiveRun(conversationId, runId)
	}
	defer func() {
		if self.ClearActiveRun != nil {
			self.ClearActiveRun(conversationId, runId)
		}
	}()

	// Broadcast user message to WebUI.
	if self.Broadcast != nil {
		self.Broadcast("conversation", map[string]interface{}{
			"state":          "user_message",
			"runId":          runId,
			"conversationId": conversationId,
			"text":           job.Message,
		})
	}

	var callbacks *agents.RunCallbacks
	if self.Broadcast != nil {
		broadcast := self.Broadcast
		callbacks = &agents.RunCallbacks{
			OnTextDelta: func(text string) {
				broadcast("chat", map[string]interface{}{
					"state":          "delta",
					"runId":          runId,
					"conversationId": conversationId,
					"text":           text,
				})
			},
			OnToolCall: func(toolName string, arguments string) {
				broadcast("chat", map[string]interface{}{
					"state":          "tool_call",
					"runId":          runId,
					"conversationId": conversationId,
					"toolName":       toolName,
					"arguments":      arguments,
				})
			},
			OnToolResult: func(toolName string, result string) {
				broadcast("chat", map[string]interface{}{
					"state":          "tool_result",
					"runId":          runId,
					"conversationId": conversationId,
					"toolName":       toolName,
					"result":         result,
				})
			},
		}
	}

	result, err := runner.Run(context.Background(), agents.RunParams{
		ConversationID: conversationId,
		Message:        job.Message,
		Model:          job.Model,
	}, callbacks)

	if self.Broadcast != nil {
		if err != nil {
			self.Broadcast("conversation", map[string]interface{}{
				"state":          "error",
				"runId":          runId,
				"conversationId": conversationId,
				"error":          err.Error(),
			})
		} else {
			payload := map[string]interface{}{
				"state":          "final",
				"runId":          runId,
				"conversationId": conversationId,
				"text":           result.Response,
				"model":          result.Model,
				"stopReason":     result.StopReason,
			}
			if result.Usage != nil {
				payload["usage"] = result.Usage
			}
			self.Broadcast("conversation", payload)
		}
	}

	now := time.Now().UnixMilli()
	if err != nil {
		log.Errorf("job %s failed: %v", job.ID, err)
		job.LastRun = now
		job.LastStatus = "error"
		job.LastError = err.Error()
	} else {
		job.LastRun = now
		job.LastStatus = "success"
		job.LastError = ""
	}

	if err := self.store.Update(job); err != nil {
		log.Errorf("updating job status: %v", err)
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

	// Self-destruct one-shot jobs after execution.
	if job.OneShot {
		if deleteError := self.store.Delete(job.ID); deleteError != nil {
			log.Errorf("deleting one-shot job %s: %v", job.ID, deleteError)
		} else {
			_ = self.Reload()
		}
		if self.Broadcast != nil {
			self.Broadcast("jobs", nil)
		}
	}
}
