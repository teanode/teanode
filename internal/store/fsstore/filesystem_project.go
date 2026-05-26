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

func (self *fileSystemTransaction) ListProjects(ctx context.Context, options *store.Option) ([]*models.Project, error) {
	return self.listProjects(options)
}

func (self *fileSystemTransaction) CreateProject(ctx context.Context, project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	return self.createProject(ctx, project, seedWorkspaceFiles, options)
}

func (self *fileSystemTransaction) GetProject(ctx context.Context, projectId string, options *store.Option) (*models.Project, error) {
	return self.getProject(projectId, options)
}

func (self *fileSystemTransaction) ModifyProject(ctx context.Context, projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	return self.modifyProject(ctx, projectId, modifier, options)
}

func (self *fileSystemTransaction) DeleteProject(ctx context.Context, projectId string, options *store.Option) error {
	return self.deleteProject(projectId, options)
}
func (self *fileSystemTransaction) listProjects(options *store.Option) ([]*models.Project, error) {
	projectConfigurations, err := self.listProjectRecords()
	if err != nil {
		return nil, err
	}
	projectConfigurations = applyOffsetLimit(projectConfigurations, options)
	projects := make([]*models.Project, 0, len(projectConfigurations))
	for _, projectConfiguration := range projectConfigurations {
		project := projectConfigurationToModel(projectConfiguration)
		projects = append(projects, &project)
	}
	return projects, nil
}

func (self *fileSystemTransaction) createProject(ctx context.Context, project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	if project == nil {
		return nil, fmt.Errorf("fsstore: project is required")
	}
	projectId := project.ID
	if projectId == "" {
		projectId = security.NewULID()
	}
	configuration := modelToProjectConfiguration(*project)
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
	createdProject := projectConfigurationToModel(configuration)
	createdProject.ID = projectId
	return &createdProject, nil
}

func (self *fileSystemTransaction) getProject(projectId string, options *store.Option) (*models.Project, error) {
	configuration, err := self.loadProjectRecord(projectId)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	project := projectConfigurationToModel(*configuration)
	project.ID = projectId
	return &project, nil
}

func (self *fileSystemTransaction) modifyProject(ctx context.Context, projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	project, err := self.GetProject(ctx, projectId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(project); err != nil {
		return nil, err
	}
	configuration := modelToProjectConfiguration(*project)
	if err := self.saveProjectRecord(projectId, &configuration); err != nil {
		return nil, err
	}
	result := projectConfigurationToModel(configuration)
	result.ID = projectId
	return &result, nil
}

func (self *fileSystemTransaction) deleteProject(projectId string, options *store.Option) error {
	projectDirectory := self.projectDirectory(projectId)
	if _, err := os.Stat(projectDirectory); errors.Is(err, os.ErrNotExist) {
		return store.ErrNotFound
	}
	return trash.Move(projectDirectory, self.trashDirectory())
}

func projectConfigurationToModel(configuration storeProjectRecord) models.Project {
	project := models.Project{
		ID:          configuration.ID,
		Name:        ptrto.TrimmedString(configuration.Name),
		Description: ptrto.TrimmedString(configuration.Description),
	}
	if !configuration.SummarizedAt.IsZero() {
		project.SummarizedAt = &configuration.SummarizedAt.Time
	}
	return project
}

func modelToProjectConfiguration(project models.Project) storeProjectRecord {
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
