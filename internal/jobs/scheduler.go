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
	RunMessage func(ctx context.Context, userId, agentId, conversationId, message, model string) (runnerId string, done <-chan struct{}, getError func() error)
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
	queryContext := ctx
	if queryContext == nil {
		queryContext = self.ctx
	}
	var job *models.Job
	transactionError := store.StoreFromContext(queryContext).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingJob, getError := transaction.GetJob(queryContext, id, nil)
		if getError != nil {
			if getError == store.ErrNotFound {
				return fmt.Errorf("job not found: %s", id)
			}
			return getError
		}
		job = existingJob
		return nil
	})
	if transactionError != nil {
		return transactionError
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
	jobModels := make([]*models.Job, 0)
	transactionError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedJobs, listError := transaction.ListJobs(ctx, "", nil)
		if listError != nil {
			return listError
		}
		jobModels = listedJobs
		return nil
	})
	if transactionError != nil {
		log.Errorf("loading jobs for tick: %v", transactionError)
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

	// Immediately disable one-shot jobs to prevent duplicate execution on the next tick.
	if job.GetOneShot() {
		enabled := false
		job.Enabled = &enabled
		_ = store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			_, modifyError := transaction.ModifyJob(ctx, job.ID, func(existingJob *models.Job) error {
				*existingJob = *job
				return nil
			}, nil)
			return modifyError
		})
	}

	// Resolve the runner for this job's agent.
	agentId := job.GetAgentID()
	if agentId == "" {
		resolveError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			existingUser, getError := transaction.GetUser(ctx, job.GetUserID(), nil)
			if getError != nil {
				return getError
			}
			defaultAgentId := existingUser.GetDefaultAgentID()
			if defaultAgentId == "" {
				return fmt.Errorf("user %s has no default agent", existingUser.ID)
			}
			agentId = defaultAgentId
			return nil
		})
		if resolveError != nil {
			log.Errorf("job %s: resolving agent failed: %v", job.ID, resolveError)
			return
		}
	}

	// Resolve conversation: use stored value if present, otherwise use default conversation.
	conversationId := job.GetConversationID()
	if conversationId == "" {
		resolveError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
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
			return nil
		})
		if resolveError != nil {
			log.Errorf("job %s: resolving conversation failed: %v", job.ID, resolveError)
			return
		}
	}
	model := job.GetModel()

	if self.RunMessage == nil {
		log.Errorf("job %s: RunMessage callback not configured", job.ID)
		return
	}

	log.Infof("executing job %s (%s) -> agent %s conversation %s", job.ID, job.GetName(), agentId, conversationId)

	runnerId, done, getError := self.RunMessage(self.ctx, job.GetUserID(), agentId, conversationId, job.GetPrompt(), model)
	log.Infof("job %s started run %s", job.ID, runnerId)

	// Wait for the run to complete.
	<-done

	lastRunAt := ptrto.TimeNowInLocal()
	if err := getError(); err != nil {
		log.Errorf("job %s failed: %v", job.ID, err)
		job.LastRunAt = lastRunAt
		job.LastStatus = ptrto.Value(models.JobStatusError)
		job.LastError = ptrto.Value(err.Error())
	} else {
		job.LastRunAt = lastRunAt
		job.LastStatus = ptrto.Value(models.JobStatusSuccess)
		job.LastError = ptrto.Value("")
	}

	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyJob(ctx, job.ID, func(existingJob *models.Job) error {
			*existingJob = *job
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		log.Errorf("updating job status: %v", err)
	}

	// Self-destruct one-shot jobs after execution.
	if job.GetOneShot() {
		deleteError := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
			return transaction.DeleteJob(ctx, job.ID, nil)
		})
		if deleteError != nil {
			log.Errorf("deleting one-shot job %s: %v", job.ID, deleteError)
		}
		if self.Broadcast != nil {
			self.Broadcast("jobs", nil)
		}
	}
}
