package db

import (
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseProjectRecord struct {
	ID          string    `gorm:"column:id;type:varchar(32);primaryKey"`
	Name        *string   `gorm:"column:name;type:varchar(256)"`
	Description *string   `gorm:"column:description"`
	CreatedAt   time.Time `gorm:"column:created_at;not null"`
	ModifiedAt  time.Time `gorm:"column:modified_at;not null"`
}

func (databaseProjectRecord) TableName() string {
	return "projects"
}

func (self *databaseTransaction) ListProjects(options *store.Option) ([]models.Project, error) {
	records := make([]databaseProjectRecord, 0)
	query := applyOption(self.database.Model(&databaseProjectRecord{}).Order("id ASC"), options)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	projects := make([]models.Project, 0, len(records))
	for _, record := range records {
		projects = append(projects, *projectRecordToModel(&record))
	}
	return projects, nil
}

func (self *databaseTransaction) CreateProject(project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	if project == nil {
		return nil, store.ErrInvalidOptions
	}
	record := modelToProjectRecord(project)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	record.CreatedAt = valueOrTime(project.CreatedAt)
	record.ModifiedAt = valueOrTime(project.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	if seedError := self.createSeedWorkspaceFiles(models.ScopeProject, record.ID, seedWorkspaceFiles, options); seedError != nil {
		return nil, seedError
	}
	return self.GetProject(record.ID, options)
}

func (self *databaseTransaction) GetProject(projectId string, options *store.Option) (*models.Project, error) {
	record := &databaseProjectRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(projectId)).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return projectRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyProject(projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	project, getError := self.GetProject(projectId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(project); modifierError != nil {
		return nil, modifierError
	}
	record := modelToProjectRecord(project)
	record.ID = strings.TrimSpace(projectId)
	record.ModifiedAt = time.Now().UTC()
	if project.CreatedAt != nil {
		record.CreatedAt = project.CreatedAt.UTC()
	}
	updateError := self.database.Model(&databaseProjectRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"name":        record.Name,
		"description": record.Description,
		"modified_at": record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	return self.GetProject(record.ID, options)
}

func (self *databaseTransaction) DeleteProject(projectId string, options *store.Option) error {
	trimmedProjectId := strings.TrimSpace(projectId)
	deleteWorkspaceError := self.database.Where("scope = ? AND scope_id = ?", string(models.ScopeProject), trimmedProjectId).Delete(&databaseWorkspaceFileRecord{}).Error
	if deleteWorkspaceError != nil {
		return databaseError(deleteWorkspaceError)
	}
	result := self.database.Where("id = ?", trimmedProjectId).Delete(&databaseProjectRecord{})
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
		ID:          strings.TrimSpace(project.ID),
		Name:        ptrto.TrimmedString(valueOrEmptyString(project.Name)),
		Description: ptrto.TrimmedString(valueOrEmptyString(project.Description)),
	}
}

func projectRecordToModel(record *databaseProjectRecord) *models.Project {
	return &models.Project{
		ID:          record.ID,
		Name:        ptrto.TrimmedString(valueOrEmptyString(record.Name)),
		Description: ptrto.TrimmedString(valueOrEmptyString(record.Description)),
		CreatedAt:   &record.CreatedAt,
		ModifiedAt:  &record.ModifiedAt,
	}
}
