package projects

import (
	"context"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

const defaultProjectDocumentName = "PROJECT.md"

func listProjects(ctx context.Context) ([]*models.Project, error) {
	projectModels := make([]*models.Project, 0)
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		listedProjects, listError := transaction.ListProjects(ctx, nil)
		if listError != nil {
			return listError
		}
		projectModels = listedProjects
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return projectModels, nil
}

func getProject(ctx context.Context, projectId string) (*models.Project, error) {
	var projectModel *models.Project
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		fetchedProject, getError := transaction.GetProject(ctx, projectId, nil)
		if getError != nil {
			return getError
		}
		projectModel = fetchedProject
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return projectModel, nil
}

func createProject(ctx context.Context, name, description, purpose string) (*models.Project, error) {
	trimmedName := name
	trimmedDescription := description
	trimmedPurpose := purpose
	if trimmedName == "" {
		return nil, fmt.Errorf("name is required")
	}

	var createdProject *models.Project
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		projectId := security.NewULID()
		seedWorkspaceFiles := make([]models.WorkspaceFile, 0)
		if trimmedPurpose != "" {
			projectMarkdown, buildError := prompts.BuildProjectMarkdown(trimmedName, projectId, trimmedDescription, trimmedPurpose)
			if buildError != nil {
				return buildError
			}
			relativePath := defaultProjectDocumentName
			contentBytes := []byte(projectMarkdown)
			seedWorkspaceFiles = append(seedWorkspaceFiles, models.WorkspaceFile{
				Path:    &relativePath,
				Content: &contentBytes,
			})
		}
		projectModel, createError := transaction.CreateProject(ctx, &models.Project{
			ID:          projectId,
			Name:        ptrto.Value(trimmedName),
			Description: ptrto.Value(trimmedDescription),
		}, seedWorkspaceFiles, nil)
		if createError != nil {
			return createError
		}
		createdProject = projectModel
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}

	return createdProject, nil
}

func renameProject(ctx context.Context, projectId, name string) (*models.Project, error) {
	trimmedName := name
	if trimmedName == "" {
		return nil, fmt.Errorf("name is required")
	}

	var updatedProject *models.Project
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		projectModel, modifyError := transaction.ModifyProject(ctx, projectId, func(projectModel *models.Project) error {
			projectModel.Name = ptrto.Value(trimmedName)
			return nil
		}, nil)
		if modifyError != nil {
			return modifyError
		}
		updatedProject = projectModel
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}

	return updatedProject, nil
}

func deleteProject(ctx context.Context, projectId string) error {
	return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteProject(ctx, projectId, nil)
	})
}
