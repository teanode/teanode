package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
)

// tickInterval is the scheduler's internal polling interval.
const tickInterval = 5 * time.Second

// Scheduler runs scheduled jobs on a periodic tick.
type Scheduler struct {
	ctx          context.Context
	mutex        sync.Mutex
	lastCronFire map[string]time.Time // tracks last fire minute per cron job to avoid duplicates
	stopChannel  chan struct{}

	Broadcast  func(event string, payload interface{})
	RunMessage func(ctx context.Context, userId, agentId, conversationId, message, providerModelName string) (runId string, done <-chan struct{}, getError func() error)
}

// NewScheduler creates a new job scheduler.
func NewScheduler(ctx context.Context) *Scheduler {
	return &Scheduler{
		ctx:          ctx,
		lastCronFire: make(map[string]time.Time),
		stopChannel:  make(chan struct{}),
	}
}

// Start loads jobs and begins the ticker goroutine.
func (self *Scheduler) Start() error {
	go self.run()
	log.Infof("job scheduler started")
	return nil
}

// Stop halts the scheduler.
func (self *Scheduler) Stop() {
	close(self.stopChannel)
}

// TriggerJob manually runs a job immediately.
func (self *Scheduler) TriggerJob(ctx context.Context, id string) error {
	var job *models.Job
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, id, nil)
		if getError != nil {
			if getError == store.ErrNotFound {
				return fmt.Errorf("jobs: job not found: %s", id)
			}
			return getError
		}
		job = existingJob
		return nil
	}); err != nil {
		return err
	}
	go self.executeJob(job)
	return nil
}

func (self *Scheduler) run() {
	defer deferutil.Recover()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-self.stopChannel:
			return
		case tickTime := <-ticker.C:
			self.tick(tickTime)
		}
	}
}

func (self *Scheduler) tick(when time.Time) {
	// Re-read the system timezone on every tick so that cron expressions are
	// evaluated against the current local time, even if the host timezone
	// changed after the process started.
	when = when.In(timeutil.LocalLocation())

	jobModels := make([]*models.Job, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedJobs, listError := transaction.ListJobs(ctx, "", nil)
		if listError != nil {
			return listError
		}
		jobModels = listedJobs
		return nil
	}); err != nil {
		log.Errorf("loading jobs for tick: %v", err)
		return
	}

	minuteBoundary := when.Truncate(time.Minute)
	nowMilliseconds := when.UnixMilli()
	for _, jobModel := range jobModels {
		if !jobModel.GetEnabled() {
			continue
		}
		if jobModel.GetUserID() == "" {
			continue
		}
		if jobModel.RunAt != nil {
			if nowMilliseconds >= jobModel.RunAt.UnixMilli() {
				// Disable before launching goroutine to prevent duplicate firing on the next tick.
				disableError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
					_, modifyError := transaction.ModifyJob(ctx, jobModel.ID, func(existingJob *models.Job) error {
						enabled := false
						existingJob.Enabled = &enabled
						return nil
					}, nil)
					return modifyError
				})
				if disableError != nil {
					log.Errorf("disabling one-shot job %s before execution: %v", jobModel.ID, disableError)
					continue
				}
				go self.executeJob(jobModel)
			}
			continue
		}
		schedule := jobModel.GetSchedule()
		expression, parseError := cronexpr.Parse(schedule)
		if parseError != nil {
			log.Errorf("bad schedule expression for job %s (%s): %v", jobModel.ID, schedule, parseError)
			continue
		}
		if expression.Matches(when) {
			self.mutex.Lock()
			lastFire := self.lastCronFire[jobModel.ID]
			alreadyFired := lastFire.Equal(minuteBoundary)
			if !alreadyFired {
				self.lastCronFire[jobModel.ID] = minuteBoundary
			}
			self.mutex.Unlock()
			if !alreadyFired {
				go self.executeJob(jobModel)
			}
		}
	}
}

func (self *Scheduler) executeJob(job *models.Job) {
	defer deferutil.Recover()

	if self.RunMessage == nil {
		log.Errorf("job %s: RunMessage callback not configured", job.ID)
		return
	}

	// Resolve agent and conversation in a single transaction.
	agentId := job.GetAgentID()
	conversationId := job.GetConversationID()
	if agentId == "" || conversationId == "" {
		if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			if agentId == "" {
				existingUser, getError := transaction.GetUser(ctx, job.GetUserID(), nil)
				if getError != nil {
					return getError
				}
				agentId = existingUser.GetDefaultAgentID()
				if agentId == "" {
					return fmt.Errorf("jobs: user %s has no default agent", existingUser.ID)
				}
			}
			if conversationId == "" {
				defaultConversation, findError := transaction.FindDefaultConversation(ctx, job.GetUserID(), agentId, nil)
				if findError == nil {
					conversationId = defaultConversation.ID
					return nil
				}
				if findError != store.ErrNotFound {
					return findError
				}
				isDefault := true
				createdConversation, createError := transaction.CreateConversation(ctx, &models.Conversation{
					ID:      security.NewULID(),
					UserID:  job.UserID,
					AgentID: ptrto.Value(agentId),
					Default: &isDefault,
				}, nil)
				if createError != nil {
					return createError
				}
				conversationId = createdConversation.ID
			}
			return nil
		}); err != nil {
			log.Errorf("job %s: resolving agent/conversation failed: %v", job.ID, err)
			return
		}
	}

	log.Infof("executing job %s (%s) -> agent %s conversation %s", job.ID, job.GetName(), agentId, conversationId)

	runId, done, getError := self.RunMessage(self.ctx, job.GetUserID(), agentId, conversationId, job.GetPrompt(), job.GetProviderModelName())
	log.Infof("job %s started run %s", job.ID, runId)

	// Wait for the run to complete.
	<-done

	// Update job status (and delete one-shot jobs) in a single transaction.
	lastRunAt := ptrto.TimeNowInLocal()
	runError := getError()
	if runError != nil {
		log.Errorf("job %s failed: %v", job.ID, runError)
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		if job.GetOneShot() {
			return transaction.DeleteJob(ctx, job.ID, nil)
		}
		_, err := transaction.ModifyJob(ctx, job.ID, func(existingJob *models.Job) error {
			existingJob.LastRunAt = lastRunAt
			if runError != nil {
				existingJob.LastStatus = ptrto.Value(models.JobStatusError)
				existingJob.LastError = ptrto.Value(runError.Error())
			} else {
				existingJob.LastStatus = ptrto.Value(models.JobStatusSuccess)
				existingJob.LastError = ptrto.Value("")
			}
			return nil
		}, nil)
		return err
	}); err != nil {
		log.Errorf("updating job %s after run: %v", job.ID, err)
	}

	if job.GetOneShot() && self.Broadcast != nil {
		self.Broadcast("jobs", nil)
	}
}
