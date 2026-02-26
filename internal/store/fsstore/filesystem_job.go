package fsstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

type filesystemJobFrontmatter struct {
	Name           string `yaml:"name"`
	Schedule       string `yaml:"schedule,omitempty"`
	Model          string `yaml:"model,omitempty"`
	AgentID        string `yaml:"agentId,omitempty"`
	Enabled        bool   `yaml:"enabled"`
	ConversationID string `yaml:"conversationId,omitempty"`
	RunAt          int64  `yaml:"runAt,omitempty"`
	OneShot        bool   `yaml:"oneShot,omitempty"`
	LastRun        int64  `yaml:"lastRun,omitempty"`
	LastStatus     string `yaml:"lastStatus,omitempty"`
	LastError      string `yaml:"lastError,omitempty"`
	CreatedAt      int64  `yaml:"createdAt"`
}

func (self *fileSystemTransaction) ListJobs(ctx context.Context, userId string, options *store.Option) ([]*models.Job, error) {
	return self.listJobs(userId, options)
}

func (self *fileSystemTransaction) CreateJob(ctx context.Context, job *models.Job, options *store.Option) (*models.Job, error) {
	return self.createJob(job, options)
}

func (self *fileSystemTransaction) GetJob(ctx context.Context, jobId string, options *store.Option) (*models.Job, error) {
	return self.getJob(jobId, options)
}

func (self *fileSystemTransaction) ModifyJob(ctx context.Context, jobId string, modifier func(*models.Job) error, options *store.Option) (*models.Job, error) {
	return self.modifyJob(ctx, jobId, modifier, options)
}

func (self *fileSystemTransaction) DeleteJob(ctx context.Context, jobId string, options *store.Option) error {
	return self.deleteJob(ctx, jobId, options)
}

func (self *fileSystemTransaction) listJobs(userId string, options *store.Option) ([]*models.Job, error) {
	if userId != "" {
		jobs, err := self.readUserJobs(userId)
		if err != nil {
			return nil, err
		}
		return applyOffsetLimitJobs(jobs, options), nil
	}

	userEntries, readError := os.ReadDir(self.usersDirectory())
	if os.IsNotExist(readError) {
		return []*models.Job{}, nil
	}
	if readError != nil {
		return nil, readError
	}

	results := make([]*models.Job, 0)
	for _, userEntry := range userEntries {
		if !userEntry.IsDir() {
			continue
		}
		jobs, err := self.readUserJobs(userEntry.Name())
		if err != nil {
			continue
		}
		results = append(results, jobs...)
	}
	return applyOffsetLimitJobs(results, options), nil
}

func (self *fileSystemTransaction) createJob(job *models.Job, options *store.Option) (*models.Job, error) {
	if job == nil || job.UserID == nil || *job.UserID == "" {
		return nil, store.ErrInvalidOptions
	}
	createdJob := *job
	if createdJob.ID == "" {
		createdJob.ID = security.NewULID()
	}
	now := ptrto.TimeNowInLocal()
	createdJob.CreatedAt = now
	createdJob.ModifiedAt = now
	if writeError := self.writeJobFile(createdJob); writeError != nil {
		return nil, writeError
	}
	return &createdJob, nil
}

func (self *fileSystemTransaction) getJob(jobId string, options *store.Option) (*models.Job, error) {
	jobsList, err := self.listJobs("", nil)
	if err != nil {
		return nil, err
	}
	for _, job := range jobsList {
		if job.ID == jobId {
			return job, nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) modifyJob(ctx context.Context, jobId string, modifier func(*models.Job) error, options *store.Option) (*models.Job, error) {
	job, err := self.GetJob(ctx, jobId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(job); err != nil {
		return nil, err
	}
	job.ID = jobId
	if job.UserID == nil || *job.UserID == "" {
		return nil, store.ErrInvalidOptions
	}
	job.ModifiedAt = ptrto.TimeNowInLocal()
	if err := self.writeJobFile(*job); err != nil {
		return nil, err
	}
	return job, nil
}

func (self *fileSystemTransaction) deleteJob(ctx context.Context, jobId string, options *store.Option) error {
	job, err := self.GetJob(ctx, jobId, options)
	if err != nil {
		return err
	}
	if job.UserID == nil || *job.UserID == "" {
		return store.ErrInvalidOptions
	}
	jobPath := filepath.Join(self.userJobsDirectory(*job.UserID), jobId+".md")
	if _, statError := os.Stat(jobPath); os.IsNotExist(statError) {
		return store.ErrNotFound
	}
	return trash.Move(jobPath, self.trashDirectory())
}

func (self *fileSystemTransaction) readUserJobs(userId string) ([]*models.Job, error) {
	jobsDirectory := self.userJobsDirectory(userId)
	entries, readError := os.ReadDir(jobsDirectory)
	if os.IsNotExist(readError) {
		return []*models.Job{}, nil
	}
	if readError != nil {
		return nil, readError
	}
	results := make([]*models.Job, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		jobId := strings.TrimSuffix(entry.Name(), ".md")
		job, parseError := self.readJobFile(userId, jobId)
		if parseError != nil {
			continue
		}
		results = append(results, &job)
	}
	return results, nil
}

func (self *fileSystemTransaction) readJobFile(userId string, jobId string) (models.Job, error) {
	data, readError := os.ReadFile(filepath.Join(self.userJobsDirectory(userId), jobId+".md"))
	if readError != nil {
		return models.Job{}, readError
	}
	return parseJobMarkdown(userId, jobId, data)
}

func (self *fileSystemTransaction) writeJobFile(job models.Job) error {
	if job.UserID == nil || *job.UserID == "" {
		return store.ErrInvalidOptions
	}
	jobsDirectory := self.userJobsDirectory(*job.UserID)
	if makeDirectoryError := os.MkdirAll(jobsDirectory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	return atomicfile.WriteFile(filepath.Join(jobsDirectory, job.ID+".md"), formatJobMarkdown(job))
}

func parseJobMarkdown(userId string, jobId string, data []byte) (models.Job, error) {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return models.Job{}, fmt.Errorf("missing frontmatter delimiter")
	}
	rest := content[4:]
	closingIndex := strings.Index(rest, "\n---\n")
	if closingIndex < 0 {
		if strings.HasSuffix(rest, "\n---") {
			closingIndex = len(rest) - 4
		} else {
			return models.Job{}, fmt.Errorf("missing closing frontmatter delimiter")
		}
	}
	frontmatterYAML := rest[:closingIndex]
	prompt := ""
	bodyStart := closingIndex + 5
	if bodyStart <= len(rest) {
		prompt = rest[bodyStart:]
	}
	var frontmatter filesystemJobFrontmatter
	if unmarshalError := yaml.Unmarshal([]byte(frontmatterYAML), &frontmatter); unmarshalError != nil {
		return models.Job{}, fmt.Errorf("parsing frontmatter: %w", unmarshalError)
	}
	return frontmatterToModelJob(userId, jobId, prompt, frontmatter), nil
}

func formatJobMarkdown(job models.Job) []byte {
	frontmatter := modelJobToFrontmatter(job)
	yamlData, _ := yaml.Marshal(frontmatter)
	var buffer bytes.Buffer
	buffer.WriteString("---\n")
	buffer.Write(yamlData)
	buffer.WriteString("---\n\n")
	buffer.WriteString(job.GetPrompt())
	buffer.WriteString("\n")
	return buffer.Bytes()
}

func frontmatterToModelJob(userId string, jobId string, prompt string, frontmatter filesystemJobFrontmatter) models.Job {
	createdAt := time.UnixMilli(frontmatter.CreatedAt)
	modifiedAt := createdAt
	var runAt *time.Time
	if frontmatter.RunAt > 0 {
		runAtTime := time.UnixMilli(frontmatter.RunAt)
		runAt = &runAtTime
	}
	var lastRunAt *time.Time
	if frontmatter.LastRun > 0 {
		lastRunTime := time.UnixMilli(frontmatter.LastRun)
		lastRunAt = &lastRunTime
	}
	return models.Job{
		ID:             jobId,
		UserID:         ptrto.TrimmedString(userId),
		Model:          ptrto.TrimmedString(frontmatter.Model),
		AgentID:        ptrto.TrimmedString(frontmatter.AgentID),
		ConversationID: ptrto.TrimmedString(frontmatter.ConversationID),
		Name:           ptrto.TrimmedString(frontmatter.Name),
		Schedule:       ptrto.TrimmedString(frontmatter.Schedule),
		Prompt:         ptrto.TrimmedString(prompt),
		Enabled:        ptrto.Value(frontmatter.Enabled),
		OneShot:        ptrto.Value(frontmatter.OneShot),
		LastStatus:     ptrto.Trimmed[models.JobStatus](frontmatter.LastStatus),
		LastError:      ptrto.TrimmedString(frontmatter.LastError),
		RunAt:          runAt,
		LastRunAt:      lastRunAt,
		CreatedAt:      &createdAt,
		ModifiedAt:     &modifiedAt,
	}
}

func modelJobToFrontmatter(job models.Job) filesystemJobFrontmatter {
	frontmatter := filesystemJobFrontmatter{
		Name:           job.GetName(),
		Schedule:       job.GetSchedule(),
		Model:          job.GetModel(),
		AgentID:        job.GetAgentID(),
		Enabled:        job.GetEnabled(),
		OneShot:        job.GetOneShot(),
		LastStatus:     string(job.GetLastStatus()),
		LastError:      job.GetLastError(),
		ConversationID: job.GetConversationID(),
	}
	if job.RunAt != nil {
		frontmatter.RunAt = job.RunAt.UnixMilli()
	}
	if job.LastRunAt != nil {
		frontmatter.LastRun = job.LastRunAt.UnixMilli()
	}
	if job.CreatedAt != nil {
		frontmatter.CreatedAt = job.CreatedAt.UnixMilli()
	} else {
		frontmatter.CreatedAt = time.Now().UnixMilli()
	}
	return frontmatter
}

func applyOffsetLimitJobs(values []*models.Job, options *store.Option) []*models.Job {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []*models.Job{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}
