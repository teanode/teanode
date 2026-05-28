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

type databaseJobRunRecord struct {
	ID                   string     `gorm:"column:id;type:varchar(32);primaryKey"`
	JobID                *string    `gorm:"column:job_id;type:varchar(32)"`
	UserID               *string    `gorm:"column:user_id;type:varchar(32)"`
	Trigger              *string    `gorm:"column:trigger;type:varchar(32)"`
	Status               *string    `gorm:"column:status;type:varchar(32)"`
	RunID                *string    `gorm:"column:run_id;type:varchar(128)"`
	Error                *string    `gorm:"column:error;type:text"`
	StartedAt            *time.Time `gorm:"column:started_at"`
	CompletedAt          *time.Time `gorm:"column:completed_at"`
	DurationMilliseconds *int64     `gorm:"column:duration_milliseconds"`
	RequestMethod        *string    `gorm:"column:request_method;type:varchar(16)"`
	RequestPath          *string    `gorm:"column:request_path;type:varchar(512)"`
	RemoteAddress        *string    `gorm:"column:remote_address;type:varchar(128)"`
}

func (self databaseJobRunRecord) TableName() string {
	return "job_runs"
}

func (self *databaseTransaction) CreateJobRun(ctx context.Context, jobRun *models.JobRun, options *store.Option) (*models.JobRun, error) {
	if jobRun == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToJobRunRecord(jobRun)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	if createError := self.database.Create(record).Error; createError != nil {
		return nil, databaseError(createError)
	}
	return jobRunRecordToModel(record), nil
}

func (self *databaseTransaction) ListJobRuns(ctx context.Context, jobId string, options *store.Option) ([]*models.JobRun, error) {
	query := self.database.Model(&databaseJobRunRecord{}).Order("started_at DESC, id DESC")
	if jobId != "" {
		query = query.Where("job_id = ?", jobId)
	}
	query = applyOption(query, options)
	records := make([]databaseJobRunRecord, 0)
	if listError := query.Find(&records).Error; listError != nil {
		return nil, databaseError(listError)
	}
	jobRuns := make([]*models.JobRun, 0, len(records))
	for _, record := range records {
		jobRuns = append(jobRuns, jobRunRecordToModel(&record))
	}
	return jobRuns, nil
}

func (self *databaseTransaction) ModifyJobRun(ctx context.Context, jobRunId string, modifier func(*models.JobRun) error, options *store.Option) (*models.JobRun, error) {
	record := &databaseJobRunRecord{}
	if getError := self.database.Where("id = ?", jobRunId).Take(record).Error; getError != nil {
		return nil, databaseError(getError)
	}
	jobRun := jobRunRecordToModel(record)
	if modifierError := modifier(jobRun); modifierError != nil {
		return nil, modifierError
	}
	updatedRecord := modelToJobRunRecord(jobRun)
	updatedRecord.ID = jobRunId
	if updateError := self.database.Model(&databaseJobRunRecord{}).Where("id = ?", jobRunId).Updates(map[string]interface{}{
		"job_id":                updatedRecord.JobID,
		"user_id":               updatedRecord.UserID,
		"trigger":               updatedRecord.Trigger,
		"status":                updatedRecord.Status,
		"run_id":                updatedRecord.RunID,
		"error":                 updatedRecord.Error,
		"started_at":            updatedRecord.StartedAt,
		"completed_at":          updatedRecord.CompletedAt,
		"duration_milliseconds": updatedRecord.DurationMilliseconds,
		"request_method":        updatedRecord.RequestMethod,
		"request_path":          updatedRecord.RequestPath,
		"remote_address":        updatedRecord.RemoteAddress,
	}).Error; updateError != nil {
		return nil, databaseError(updateError)
	}
	return jobRun, nil
}

func modelToJobRunRecord(jobRun *models.JobRun) *databaseJobRunRecord {
	return &databaseJobRunRecord{
		ID:                   jobRun.ID,
		JobID:                ptrto.TrimmedString(jobRun.GetJobID()),
		UserID:               ptrto.TrimmedString(jobRun.GetUserID()),
		Trigger:              ptrto.TrimmedString(string(jobRun.GetTrigger())),
		Status:               ptrto.TrimmedString(string(jobRun.GetStatus())),
		RunID:                ptrto.TrimmedString(jobRun.GetRunID()),
		Error:                ptrto.TrimmedString(jobRun.GetError()),
		StartedAt:            jobRun.StartedAt,
		CompletedAt:          jobRun.CompletedAt,
		DurationMilliseconds: jobRun.DurationMilliseconds,
		RequestMethod:        ptrto.TrimmedString(jobRun.GetRequestMethod()),
		RequestPath:          ptrto.TrimmedString(jobRun.GetRequestPath()),
		RemoteAddress:        ptrto.TrimmedString(jobRun.GetRemoteAddress()),
	}
}

func jobRunRecordToModel(record *databaseJobRunRecord) *models.JobRun {
	return &models.JobRun{
		ID:                   record.ID,
		JobID:                ptrto.TrimmedString(valueor.Zero(record.JobID)),
		UserID:               ptrto.TrimmedString(valueor.Zero(record.UserID)),
		Trigger:              ptrto.Trimmed[models.JobTriggerKind](valueor.Zero(record.Trigger)),
		Status:               ptrto.Trimmed[models.JobRunStatus](valueor.Zero(record.Status)),
		RunID:                ptrto.TrimmedString(valueor.Zero(record.RunID)),
		Error:                ptrto.TrimmedString(valueor.Zero(record.Error)),
		StartedAt:            record.StartedAt,
		CompletedAt:          record.CompletedAt,
		DurationMilliseconds: record.DurationMilliseconds,
		RequestMethod:        ptrto.TrimmedString(valueor.Zero(record.RequestMethod)),
		RequestPath:          ptrto.TrimmedString(valueor.Zero(record.RequestPath)),
		RemoteAddress:        ptrto.TrimmedString(valueor.Zero(record.RemoteAddress)),
	}
}
