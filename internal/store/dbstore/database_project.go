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

type databaseProjectRecord struct {
	ID           string     `gorm:"column:id;type:varchar(32);primaryKey"`
	Name         *string    `gorm:"column:name;type:varchar(256)"`
	Description  *string    `gorm:"column:description"`
	SummarizedAt *time.Time `gorm:"column:summarized_at"`
	CreatedAt    time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt   time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseProjectRecord) TableName() string {
	return "projects"
}

func (self *databaseTransaction) ListProjects(ctx context.Context, options *store.Option) ([]*models.Project, error) {
	records := make([]databaseProjectRecord, 0)
	query := applyOption(self.database.Model(&databaseProjectRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	projects := make([]*models.Project, 0, len(records))
	for _, record := range records {
		projects = append(projects, projectRecordToModel(&record))
	}
	return projects, nil
}

func (self *databaseTransaction) CreateProject(ctx context.Context, project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	if project == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToProjectRecord(project)
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
	if seedError := self.createSeedWorkspaceFiles(ctx, models.ScopeProject, record.ID, seedWorkspaceFiles, options); seedError != nil {
		return nil, seedError
	}
	return self.GetProject(ctx, record.ID, options)
}

func (self *databaseTransaction) GetProject(ctx context.Context, projectId string, options *store.Option) (*models.Project, error) {
	record := &databaseProjectRecord{}
	getError := self.database.Where("id = ?", projectId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return projectRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyProject(ctx context.Context, projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	project, getError := self.GetProject(ctx, projectId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(project); modifierError != nil {
		return nil, modifierError
	}
	record := modelToProjectRecord(project)
	record.ID = projectId
	record.ModifiedAt = *ptrto.TimeNowInLocal()
	updateError := self.database.Model(&databaseProjectRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"name":           record.Name,
		"description":    record.Description,
		"summarized_at":  record.SummarizedAt,
		"modified_at":    record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetProject(ctx, record.ID, options)
}

func (self *databaseTransaction) DeleteProject(ctx context.Context, projectId string, options *store.Option) error {
	deleteWorkspaceFileError := self.database.Where("scope = ? AND scope_id = ?", string(models.ScopeProject), projectId).Delete(&databaseWorkspaceFileRecord{}).Error
	if deleteWorkspaceFileError != nil {
		return databaseError(deleteWorkspaceFileError)
	}
	result := self.database.Where("id = ?", projectId).Delete(&databaseProjectRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func modelToProjectRecord(project *models.Project) *databaseProjectRecord {
	return &databaseProjectRecord{
		ID:           project.ID,
		Name:         ptrto.TrimmedString(project.GetName()),
		Description:  ptrto.TrimmedString(project.GetDescription()),
		SummarizedAt: project.SummarizedAt,
	}
}

func projectRecordToModel(record *databaseProjectRecord) *models.Project {
	return &models.Project{
		ID:           record.ID,
		Name:         ptrto.TrimmedString(valueor.Zero(record.Name)),
		Description:  ptrto.TrimmedString(valueor.Zero(record.Description)),
		SummarizedAt: record.SummarizedAt,
		CreatedAt:    &record.CreatedAt,
		ModifiedAt:   &record.ModifiedAt,
	}
}
