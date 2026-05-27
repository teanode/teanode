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

type TriggerMetadata struct {
	Trigger       models.JobTriggerKind
	RequestMethod string
	RequestPath   string
	RemoteAddress string
}

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
	_, err := self.TriggerJobWithMetadata(ctx, id, TriggerMetadata{Trigger: models.JobTriggerKindManual})
	return err
}

func (self *Scheduler) TriggerJobWithMetadata(ctx context.Context, id string, metadata TriggerMetadata) (*models.JobRun, error) {
	if metadata.Trigger == "" {
		metadata.Trigger = models.JobTriggerKindManual
	}
	var job *models.Job
	var jobRun *models.JobRun
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(ctx, id, nil)
		if getError != nil {
			if getError == store.ErrNotFound {
				return fmt.Errorf("jobs: job not found: %s", id)
			}
			return getError
		}
		job = existingJob
		startedAt := ptrto.TimeNowInLocal()
		var createError error
		jobRun, createError = transaction.CreateJobRun(ctx, &models.JobRun{
			JobID:         ptrto.Value(job.ID),
			UserID:        ptrto.Value(job.GetUserID()),
			Trigger:       ptrto.Value(metadata.Trigger),
			Status:        ptrto.Value(models.JobRunStatusRunning),
			StartedAt:     startedAt,
			RequestMethod: ptrto.TrimmedString(metadata.RequestMethod),
			RequestPath:   ptrto.TrimmedString(metadata.RequestPath),
			RemoteAddress: ptrto.TrimmedString(metadata.RemoteAddress),
		}, nil)
		if createError != nil {
			return createError
		}
		return nil
	}); err != nil {
		return nil, err
	}
	go self.executeJob(job, jobRun)
	return jobRun, nil
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
		if jobModel.GetTrigger() == models.JobTriggerKindWebhook {
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
				if _, triggerError := self.TriggerJobWithMetadata(self.ctx, jobModel.ID, TriggerMetadata{
					Trigger: models.JobTriggerKindScheduled,
				}); triggerError != nil {
					log.Errorf("triggering scheduled one-shot job %s: %v", jobModel.ID, triggerError)
				}
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
				if _, triggerError := self.TriggerJobWithMetadata(self.ctx, jobModel.ID, TriggerMetadata{
					Trigger: models.JobTriggerKindScheduled,
				}); triggerError != nil {
					log.Errorf("triggering scheduled job %s: %v", jobModel.ID, triggerError)
				}
			}
		}
	}
}

func (self *Scheduler) executeJob(job *models.Job, jobRun *models.JobRun) {
	defer deferutil.Recover()

	if self.RunMessage == nil {
		log.Errorf("job %s: RunMessage callback not configured", job.ID)
		self.completeJobRun(job, jobRun, "", fmt.Errorf("jobs: RunMessage callback not configured"))
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
			self.completeJobRun(job, jobRun, "", err)
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
		if jobRun != nil {
			completedAt := ptrto.TimeNowInLocal()
			durationMilliseconds := completedAt.UnixMilli() - jobRun.GetStartedAt().UnixMilli()
			_, updateError := transaction.ModifyJobRun(ctx, jobRun.ID, func(existingJobRun *models.JobRun) error {
				existingJobRun.Status = ptrto.Value(models.JobRunStatusSuccess)
				existingJobRun.Error = nil
				if runError != nil {
					existingJobRun.Status = ptrto.Value(models.JobRunStatusError)
					existingJobRun.Error = ptrto.Value(runError.Error())
				}
				existingJobRun.RunID = ptrto.TrimmedString(runId)
				existingJobRun.CompletedAt = completedAt
				existingJobRun.DurationMilliseconds = ptrto.Value(durationMilliseconds)
				return nil
			}, nil)
			if updateError != nil {
				return updateError
			}
		}
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

func (self *Scheduler) completeJobRun(job *models.Job, jobRun *models.JobRun, runId string, runError error) {
	if jobRun == nil {
		return
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		completedAt := ptrto.TimeNowInLocal()
		durationMilliseconds := completedAt.UnixMilli() - jobRun.GetStartedAt().UnixMilli()
		_, updateError := transaction.ModifyJobRun(ctx, jobRun.ID, func(existingJobRun *models.JobRun) error {
			existingJobRun.RunID = ptrto.TrimmedString(runId)
			existingJobRun.CompletedAt = completedAt
			existingJobRun.DurationMilliseconds = ptrto.Value(durationMilliseconds)
			existingJobRun.Status = ptrto.Value(models.JobRunStatusSuccess)
			existingJobRun.Error = nil
			if runError != nil {
				existingJobRun.Status = ptrto.Value(models.JobRunStatusError)
				existingJobRun.Error = ptrto.Value(runError.Error())
			}
			return nil
		}, nil)
		return updateError
	}); err != nil {
		log.Errorf("updating job run %s after failure for job %s: %v", jobRun.ID, job.ID, err)
	}
}
