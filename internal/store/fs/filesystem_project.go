package fs

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
)

func (self *transaction) ListProjects(options *store.Option) ([]models.Project, error) {
	return self.listProjects(options)
}

func (self *transaction) CreateProject(project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	return self.createProject(project, seedWorkspaceFiles, options)
}

func (self *transaction) GetProject(projectId string, options *store.Option) (*models.Project, error) {
	return self.getProject(projectId, options)
}

func (self *transaction) ModifyProject(projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	return self.modifyProject(projectId, modifier, options)
}

func (self *transaction) DeleteProject(projectId string, options *store.Option) error {
	return self.deleteProject(projectId, options)
}
func (self *transaction) listProjects(options *store.Option) ([]models.Project, error) {
	projectConfigurations, err := self.listProjectRecords()
	if err != nil {
		return nil, err
	}
	projectConfigurations = applyOffsetLimitProjectConfig(projectConfigurations, options)
	projects := make([]models.Project, 0, len(projectConfigurations))
	for _, projectConfiguration := range projectConfigurations {
		projects = append(projects, projectConfigToModel(projectConfiguration))
	}
	return projects, nil
}

func (self *transaction) createProject(project *models.Project, seedWorkspaceFiles []models.WorkspaceFile, options *store.Option) (*models.Project, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	projectId := strings.TrimSpace(project.ID)
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
		if _, err := self.CreateWorkspaceFile(&copyFile, options); err != nil {
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

func (self *transaction) modifyProject(projectId string, modifier func(*models.Project) error, options *store.Option) (*models.Project, error) {
	project, err := self.GetProject(projectId, options)
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
	return models.Project{
		ID:          strings.TrimSpace(configuration.ID),
		Name:        ptrto.TrimmedString(configuration.Name),
		Description: ptrto.TrimmedString(configuration.Description),
	}
}

func modelToProjectConfig(project models.Project) storeProjectRecord {
	return storeProjectRecord{
		ID:          strings.TrimSpace(project.ID),
		Name:        strings.TrimSpace(valueOrEmpty(project.Name)),
		Description: strings.TrimSpace(valueOrEmpty(project.Description)),
	}
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
