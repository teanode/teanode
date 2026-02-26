package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/valueor"
)

type databaseJobRecord struct {
	ID             string     `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID         *string    `gorm:"column:user_id;type:varchar(32)"`
	Model          *string    `gorm:"column:model;type:varchar(128)"`
	AgentID        *string    `gorm:"column:agent_id;type:varchar(32)"`
	ConversationID *string    `gorm:"column:conversation_id;type:varchar(32)"`
	Name           *string    `gorm:"column:name;type:varchar(256)"`
	Schedule       *string    `gorm:"column:schedule;type:varchar(128)"`
	Prompt         *string    `gorm:"column:prompt"`
	Enabled        *bool      `gorm:"column:enabled"`
	OneShot        *bool      `gorm:"column:one_shot"`
	LastStatus     *string    `gorm:"column:last_status"`
	LastError      *string    `gorm:"column:last_error"`
	RunAt          *time.Time `gorm:"column:run_at"`
	LastRunAt      *time.Time `gorm:"column:last_run_at"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt     time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseJobRecord) TableName() string {
	return "jobs"
}

func (self *databaseTransaction) ListJobs(ctx context.Context, userId string, options *store.Option) ([]*models.Job, error) {
	query := self.database.Model(&databaseJobRecord{})
	if userId != "" {
		query = query.Where("user_id = ?", userId)
	}
	query = applyOption(query.Order("id ASC"), options)
	records := make([]databaseJobRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	jobs := make([]*models.Job, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, jobRecordToModel(&record))
	}
	return jobs, nil
}

func (self *databaseTransaction) CreateJob(ctx context.Context, job *models.Job, options *store.Option) (*models.Job, error) {
	if job == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToJobRecord(job)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	now := ptrto.TimeNowInLocal()
	record.CreatedAt = *now
	record.ModifiedAt = *now
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetJob(ctx, record.ID, options)
}

func (self *databaseTransaction) GetJob(ctx context.Context, jobId string, options *store.Option) (*models.Job, error) {
	record := &databaseJobRecord{}
	getError := self.database.Where("id = ?", jobId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return jobRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyJob(ctx context.Context, jobId string, modifier func(*models.Job) error, options *store.Option) (*models.Job, error) {
	job, getError := self.GetJob(ctx, jobId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(job); modifierError != nil {
		return nil, modifierError
	}
	record := modelToJobRecord(job)
	record.ID = jobId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseJobRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":         record.UserID,
		"model":           record.Model,
		"agent_id":        record.AgentID,
		"conversation_id": record.ConversationID,
		"name":            record.Name,
		"schedule":        record.Schedule,
		"prompt":          record.Prompt,
		"enabled":         record.Enabled,
		"one_shot":        record.OneShot,
		"last_status":     record.LastStatus,
		"last_error":      record.LastError,
		"run_at":          record.RunAt,
		"last_run_at":     record.LastRunAt,
		"modified_at":     record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetJob(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteJob(ctx context.Context, jobId string, options *store.Option) error {
	result := self.database.Where("id = ?", jobId).Delete(&databaseJobRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToJobRecord(job *models.Job) *databaseJobRecord {
	return &databaseJobRecord{
		ID:             job.ID,
		UserID:         ptrto.TrimmedString(job.GetUserID()),
		Model:          ptrto.TrimmedString(job.GetModel()),
		AgentID:        ptrto.TrimmedString(job.GetAgentID()),
		ConversationID: ptrto.TrimmedString(job.GetConversationID()),
		Name:           ptrto.TrimmedString(job.GetName()),
		Schedule:       ptrto.TrimmedString(job.GetSchedule()),
		Prompt:         ptrto.TrimmedString(job.GetPrompt()),
		Enabled:        job.Enabled,
		OneShot:        job.OneShot,
		LastStatus:     ptrto.TrimmedString(string(job.GetLastStatus())),
		LastError:      ptrto.TrimmedString(job.GetLastError()),
		RunAt:          job.RunAt,
		LastRunAt:      job.LastRunAt,
	}
}

func jobRecordToModel(record *databaseJobRecord) *models.Job {
	return &models.Job{
		ID:             record.ID,
		UserID:         ptrto.TrimmedString(valueor.Zero(record.UserID)),
		Model:          ptrto.TrimmedString(valueor.Zero(record.Model)),
		AgentID:        ptrto.TrimmedString(valueor.Zero(record.AgentID)),
		ConversationID: ptrto.TrimmedString(valueor.Zero(record.ConversationID)),
		Name:           ptrto.TrimmedString(valueor.Zero(record.Name)),
		Schedule:       ptrto.TrimmedString(valueor.Zero(record.Schedule)),
		Prompt:         ptrto.TrimmedString(valueor.Zero(record.Prompt)),
		Enabled:        record.Enabled,
		OneShot:        record.OneShot,
		LastStatus:     ptrto.Trimmed[models.JobStatus](valueor.Zero(record.LastStatus)),
		LastError:      ptrto.TrimmedString(valueor.Zero(record.LastError)),
		RunAt:          record.RunAt,
		LastRunAt:      record.LastRunAt,
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
}
