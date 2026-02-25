package db

import (
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
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

func (self *databaseTransaction) ListJobs(userId string, options *store.Option) ([]models.Job, error) {
	query := self.database.Model(&databaseJobRecord{})
	if strings.TrimSpace(userId) != "" {
		query = query.Where("user_id = ?", strings.TrimSpace(userId))
	}
	query = applyOption(query.Order("id ASC"), options)
	records := make([]databaseJobRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	jobs := make([]models.Job, 0, len(records))
	for _, record := range records {
		jobs = append(jobs, *jobRecordToModel(&record))
	}
	return jobs, nil
}

func (self *databaseTransaction) CreateJob(job *models.Job, options *store.Option) (*models.Job, error) {
	if job == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToJobRecord(job)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	record.CreatedAt = valueOrTime(job.CreatedAt)
	record.ModifiedAt = valueOrTime(job.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return self.GetJob(record.ID, options)
}

func (self *databaseTransaction) GetJob(jobId string, options *store.Option) (*models.Job, error) {
	record := &databaseJobRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(jobId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return jobRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyJob(jobId string, modifier func(*models.Job) error, options *store.Option) (*models.Job, error) {
	job, getError := self.GetJob(jobId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(job); modifierError != nil {
		return nil, modifierError
	}
	record := modelToJobRecord(job)
	record.ID = strings.TrimSpace(jobId)
	record.ModifiedAt = time.Now().UTC()
	if job.CreatedAt != nil {
		record.CreatedAt = job.CreatedAt.UTC()
	}
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
	return self.GetJob(record.ID, options)
}

func (self *databaseTransaction) DeleteJob(jobId string, options *store.Option) error {
	result := self.database.Where("id = ?", strings.TrimSpace(jobId)).Delete(&databaseJobRecord{})
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
		ID:             strings.TrimSpace(job.ID),
		UserID:         ptrto.TrimmedString(valueOrEmptyString(job.UserID)),
		Model:          ptrto.TrimmedString(valueOrEmptyString(job.Model)),
		AgentID:        ptrto.TrimmedString(valueOrEmptyString(job.AgentID)),
		ConversationID: ptrto.TrimmedString(valueOrEmptyString(job.ConversationID)),
		Name:           ptrto.TrimmedString(valueOrEmptyString(job.Name)),
		Schedule:       ptrto.TrimmedString(valueOrEmptyString(job.Schedule)),
		Prompt:         ptrto.TrimmedString(valueOrEmptyString(job.Prompt)),
		Enabled:        job.Enabled,
		OneShot:        job.OneShot,
		LastStatus:     ptrto.TrimmedString(valueOrEmptyString(job.LastStatus)),
		LastError:      ptrto.TrimmedString(valueOrEmptyString(job.LastError)),
		RunAt:          job.RunAt,
		LastRunAt:      job.LastRunAt,
	}
}

func jobRecordToModel(record *databaseJobRecord) *models.Job {
	return &models.Job{
		ID:             record.ID,
		UserID:         ptrto.TrimmedString(valueOrEmptyString(record.UserID)),
		Model:          ptrto.TrimmedString(valueOrEmptyString(record.Model)),
		AgentID:        ptrto.TrimmedString(valueOrEmptyString(record.AgentID)),
		ConversationID: ptrto.TrimmedString(valueOrEmptyString(record.ConversationID)),
		Name:           ptrto.TrimmedString(valueOrEmptyString(record.Name)),
		Schedule:       ptrto.TrimmedString(valueOrEmptyString(record.Schedule)),
		Prompt:         ptrto.TrimmedString(valueOrEmptyString(record.Prompt)),
		Enabled:        record.Enabled,
		OneShot:        record.OneShot,
		LastStatus:     ptrto.TrimmedString(valueOrEmptyString(record.LastStatus)),
		LastError:      ptrto.TrimmedString(valueOrEmptyString(record.LastError)),
		RunAt:          record.RunAt,
		LastRunAt:      record.LastRunAt,
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
}
