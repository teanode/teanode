package projects

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/valueor"
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

func touch(ctx context.Context, projectId string) error {
	return store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyProject(ctx, projectId, func(projectModel *models.Project) error {
			now := time.Now()
			projectModel.ModifiedAt = &now
			return nil
		}, nil)
		return modifyError
	})
}

func listFiles(ctx context.Context, projectId string) ([]string, error) {
	files := make([]string, 0)
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		workspaceFiles, listError := transaction.ListWorkspaceFilesByPath(ctx, models.ScopeProject, projectId, "", nil)
		if listError != nil {
			return listError
		}
		files = make([]string, 0, len(workspaceFiles))
		for _, workspaceFile := range workspaceFiles {
			path := workspaceFile.GetPath()
			if path != "" {
				files = append(files, path)
			}
		}
		sort.Strings(files)
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}
	return files, nil
}

func readFile(ctx context.Context, projectId, path string) (string, error) {
	normalizedPath, normalizeError := normalizeRelativePath(path)
	if normalizeError != nil {
		return "", normalizeError
	}

	content := ""
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		workspaceFile, getError := transaction.GetWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedPath, nil)
		if getError != nil {
			return getError
		}
		if workspaceFile.Content == nil {
			content = ""
			return nil
		}
		content = string(*workspaceFile.Content)
		return nil
	}); transactionError != nil {
		return "", transactionError
	}
	return content, nil
}

func writeFile(ctx context.Context, projectId, path, content string) error {
	normalizedPath, normalizeError := normalizeRelativePath(path)
	if normalizeError != nil {
		return normalizeError
	}

	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		contentBytes := []byte(content)
		_, modifyError := transaction.ModifyWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedPath, func(workspaceFile *models.WorkspaceFile) error {
			workspaceFile.Content = &contentBytes
			return nil
		}, nil)
		if modifyError == nil {
			return nil
		}
		if modifyError != store.ErrNotFound {
			return modifyError
		}
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   ptrto.Value(models.ScopeProject),
			ScopeID: ptrto.Value(projectId),
			Path:    ptrto.Value(normalizedPath),
			Content: &contentBytes,
		}, nil)
		return createError
	}); transactionError != nil {
		return transactionError
	}

	return touch(ctx, projectId)
}

func appendFile(ctx context.Context, projectId, path, content string) error {
	normalizedPath, normalizeError := normalizeRelativePath(path)
	if normalizeError != nil {
		return normalizeError
	}

	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingContent := ""
		workspaceFile, getError := transaction.GetWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedPath, nil)
		if getError == nil && workspaceFile.Content != nil {
			existingContent = string(*workspaceFile.Content)
		}
		nextContent := existingContent + content + "\n"
		contentBytes := []byte(nextContent)
		if getError == nil {
			_, modifyError := transaction.ModifyWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedPath, func(existingWorkspaceFile *models.WorkspaceFile) error {
				existingWorkspaceFile.Content = &contentBytes
				return nil
			}, nil)
			return modifyError
		}
		if getError != store.ErrNotFound {
			return getError
		}
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   ptrto.Value(models.ScopeProject),
			ScopeID: ptrto.Value(projectId),
			Path:    ptrto.Value(normalizedPath),
			Content: &contentBytes,
		}, nil)
		return createError
	}); transactionError != nil {
		return transactionError
	}

	return touch(ctx, projectId)
}

type searchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func searchFiles(ctx context.Context, projectId, query string, maxResults int) ([]searchMatch, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	matches := make([]searchMatch, 0)
	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		limit := uint64(maxResults)
		includeContent := true
		searchResults, searchError := transaction.SearchWorkspaceFiles(ctx, models.ScopeProject, projectId, query, store.WorkspaceSearchOptions{
			Limit:          &limit,
			IncludeContent: &includeContent,
		}, nil)
		if searchError != nil {
			return searchError
		}
		for _, searchResult := range searchResults {
			path := valueor.Zero(searchResult.Path)
			if path == "" || (!strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".txt")) {
				continue
			}
			matchedLines := valueor.Zero(searchResult.MatchedLines)
			for index, line := range matchedLines {
				matches = append(matches, searchMatch{
					Path: path,
					Line: index + 1,
					Text: line,
				})
				if len(matches) >= maxResults {
					return nil
				}
			}
		}
		return nil
	}); transactionError != nil {
		return nil, transactionError
	}

	return matches, nil
}

func deleteFile(ctx context.Context, projectId, path string) error {
	normalizedPath, normalizeError := normalizeRelativePath(path)
	if normalizeError != nil {
		return normalizeError
	}

	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		return transaction.DeleteWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedPath, nil)
	}); transactionError != nil {
		return transactionError
	}

	return touch(ctx, projectId)
}

func moveFile(ctx context.Context, projectId, fromPath, toPath string) error {
	normalizedFromPath, fromError := normalizeRelativePath(fromPath)
	if fromError != nil {
		return fromError
	}
	normalizedToPath, toError := normalizeRelativePath(toPath)
	if toError != nil {
		return toError
	}
	if normalizedFromPath == normalizedToPath {
		return nil
	}

	if transactionError := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		sourceFile, getError := transaction.GetWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedFromPath, nil)
		if getError != nil {
			return getError
		}
		contentBytes := []byte{}
		if sourceFile.Content != nil {
			contentBytes = append(contentBytes, (*sourceFile.Content)...)
		}
		targetPath := normalizedToPath
		_, createError := transaction.CreateWorkspaceFile(ctx, &models.WorkspaceFile{
			Scope:   ptrto.Value(models.ScopeProject),
			ScopeID: ptrto.Value(projectId),
			Path:    &targetPath,
			Content: &contentBytes,
		}, nil)
		if createError != nil {
			if createError != store.ErrAlreadyExists {
				return createError
			}
			_, modifyError := transaction.ModifyWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedToPath, func(existingWorkspaceFile *models.WorkspaceFile) error {
				existingWorkspaceFile.Content = &contentBytes
				return nil
			}, nil)
			if modifyError != nil {
				return modifyError
			}
		}
		return transaction.DeleteWorkspaceFileByPath(ctx, models.ScopeProject, projectId, normalizedFromPath, nil)
	}); transactionError != nil {
		return transactionError
	}

	return touch(ctx, projectId)
}

func normalizeRelativePath(relativePath string) (string, error) {
	cleanedPath := filepath.Clean(relativePath)
	if cleanedPath == "." || cleanedPath == "" || filepath.IsAbs(cleanedPath) || cleanedPath == ".." || strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path: %s", relativePath)
	}
	return cleanedPath, nil
}
