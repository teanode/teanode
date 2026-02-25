package fsstore

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/trash"
)

func (self *transaction) ListProjects(ctx context.Context, options *store.Option) ([]*models.Project, error) {
	return self.listProjects(options)
}

func (self *transaction) CreateProject(ctx context.Context, project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	return self.createProject(ctx, project, seedWorkspaceFiles, options)
}

func (self *transaction) GetProject(ctx context.Context, projectId string, options *store.Option) (*models.Project, error) {
	return self.getProject(projectId, options)
}

func (self *transaction) ModifyProject(ctx context.Context, projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	return self.modifyProject(ctx, projectId, modifier, options)
}

func (self *transaction) DeleteProject(ctx context.Context, projectId string, options *store.Option) error {
	return self.deleteProject(projectId, options)
}
func (self *transaction) listProjects(options *store.Option) ([]*models.Project, error) {
	projectConfigurations, err := self.listProjectRecords()
	if err != nil {
		return nil, err
	}
	projectConfigurations = applyOffsetLimitProjectConfig(projectConfigurations, options)
	projects := make([]*models.Project, 0, len(projectConfigurations))
	for _, projectConfiguration := range projectConfigurations {
		project := projectConfigToModel(projectConfiguration)
		projects = append(projects, &project)
	}
	return projects, nil
}

func (self *transaction) createProject(ctx context.Context, project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	projectId := project.ID
	if projectId == "" {
		projectId = security.NewULID()
	}
	configuration := modelToProjectConfig(*project)
	if err := self.saveProjectRecord(projectId, &configuration); err != nil {
		return nil, err
	}
	for _, file := range seedWorkspaceFiles {
		copyFile := file
		scope := models.ScopeProject
		copyFile.Scope = &scope
		copyFile.ScopeID = &projectId
		if _, err := self.CreateWorkspaceFile(ctx, &copyFile, options); err != nil {
			return nil, err
		}
	}
	createdProject := projectConfigToModel(configuration)
	createdProject.ID = projectId
	return &createdProject, nil
}

func (self *transaction) getProject(projectId string, options *store.Option) (*models.Project, error) {
	configuration, err := self.loadProjectRecord(projectId)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	project := projectConfigToModel(*configuration)
	project.ID = projectId
	return &project, nil
}

func (self *transaction) modifyProject(ctx context.Context, projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	project, err := self.GetProject(ctx, projectId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(project); err != nil {
		return nil, err
	}
	configuration := modelToProjectConfig(*project)
	if err := self.saveProjectRecord(projectId, &configuration); err != nil {
		return nil, err
	}
	result := projectConfigToModel(configuration)
	result.ID = projectId
	return &result, nil
}

func (self *transaction) deleteProject(projectId string, options *store.Option) error {
	projectDirectory := self.projectDirectory(projectId)
	if _, err := os.Stat(projectDirectory); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return trash.Move(projectDirectory, self.trashDirectory())
}

func projectConfigToModel(configuration storeProjectRecord) models.Project {
	project := models.Project{
		ID:          configuration.ID,
		Name:        ptrto.TrimmedString(configuration.Name),
		Description: ptrto.TrimmedString(configuration.Description),
	}
	if !configuration.SummarizedAt.Time.IsZero() {
		project.SummarizedAt = &configuration.SummarizedAt.Time
	}
	return project
}

func modelToProjectConfig(project models.Project) storeProjectRecord {
	record := storeProjectRecord{
		ID:          project.ID,
		Name:        project.GetName(),
		Description: project.GetDescription(),
	}
	if project.SummarizedAt != nil {
		record.SummarizedAt = timeutil.Timestamp{Time: *project.SummarizedAt}
	}
	return record
}

func applyOffsetLimitProjectConfig(values []storeProjectRecord, options *store.Option) []storeProjectRecord {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []storeProjectRecord{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}
