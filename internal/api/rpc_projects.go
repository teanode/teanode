package api

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func (self *webSocketConnection) handleProjectsList(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	projectList := make([]map[string]interface{}, 0)
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		projects, err := transaction.ListProjects(ctx, nil)
		if err != nil {
			return err
		}
		for _, project := range projects {
			projectList = append(projectList, map[string]interface{}{
				"id":          project.ID,
				"name":        project.GetName(),
				"description": project.GetDescription(),
			})
		}
		return nil
	}); err != nil {
		return nil, rpcError(500, "listing projects: "+err.Error())
	}
	return map[string]interface{}{
		"projects": projectList,
	}, nil
}

type projectsCreateParameters struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
}

func projectRpcError(err error, operation string) *rpcHandlerError {
	message := err.Error()
	lower := strings.ToLower(message)
	if errors.Is(err, os.ErrNotExist) {
		return rpcError(404, operation+": not found")
	}
	if strings.Contains(lower, "not found") {
		return rpcError(404, operation+": "+message)
	}
	if strings.Contains(lower, "invalid projectid") || strings.Contains(lower, "name is required") {
		return rpcError(400, operation+": "+message)
	}
	return rpcError(500, operation+": "+message)
}

func (self *webSocketConnection) handleProjectsCreate(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[projectsCreateParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.Name == "" {
		return nil, rpcError(400, "name is required")
	}
	var createdProject *models.Project
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		projectId := security.NewULID()
		projectMarkdown, buildError := prompts.BuildProjectMarkdown(parameters.Name, projectId, parameters.Description, parameters.Purpose)
		if buildError != nil {
			return buildError
		}
		relativePath := "PROJECT.md"
		contentBytes := []byte(projectMarkdown)
		workspaceFiles := []models.WorkspaceFile{
			{Path: &relativePath, Content: &contentBytes},
		}
		project, err := transaction.CreateProject(ctx, &models.Project{
			ID:          projectId,
			Name:        ptrto.TrimmedString(parameters.Name),
			Description: ptrto.TrimmedString(parameters.Description),
		}, workspaceFiles, nil)
		if err != nil {
			return err
		}
		createdProject = project
		return nil
	}); err != nil {
		return nil, projectRpcError(err, "creating project")
	}
	return map[string]interface{}{
		"project": map[string]interface{}{
			"id":          createdProject.ID,
			"name":        createdProject.GetName(),
			"description": createdProject.GetDescription(),
		},
	}, nil
}

type projectsRenameParameters struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

func (self *webSocketConnection) handleProjectsRename(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[projectsRenameParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.ProjectID == "" {
		return nil, rpcError(400, "projectId is required")
	}
	if parameters.Name == "" {
		return nil, rpcError(400, "name is required")
	}
	var updatedProject *models.Project
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		project, err := transaction.ModifyProject(ctx, parameters.ProjectID, func(project *models.Project) error {
			project.Name = ptrto.TrimmedString(parameters.Name)
			return nil
		}, nil)
		if err != nil {
			return err
		}
		updatedProject = project
		return nil
	}); err != nil {
		return nil, projectRpcError(err, "renaming project")
	}
	return map[string]interface{}{
		"project": map[string]interface{}{
			"id":          updatedProject.ID,
			"name":        updatedProject.GetName(),
			"description": updatedProject.GetDescription(),
		},
	}, nil
}

type projectsDeleteParameters struct {
	ProjectID string `json:"projectId"`
}

func (self *webSocketConnection) handleProjectsDelete(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[projectsDeleteParameters](frame)
	if err != nil {
		return nil, err
	}
	if parameters.ProjectID == "" {
		return nil, rpcError(400, "projectId is required")
	}
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteProject(ctx, parameters.ProjectID, nil)
	}); err != nil {
		return nil, projectRpcError(err, "deleting project")
	}
	return map[string]interface{}{
		"deleted": true,
	}, nil
}
