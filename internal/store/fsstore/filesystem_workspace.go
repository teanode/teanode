package fsstore

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
)

func (self *fileSystemTransaction) CreateWorkspaceFile(ctx context.Context, file *models.WorkspaceFile, options *store.Option) (*models.WorkspaceFile, error) {
	return self.createWorkspaceFile(file, options)
}

func (self *fileSystemTransaction) GetWorkspaceFileByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, options *store.Option) (*models.WorkspaceFile, error) {
	return self.getWorkspaceFile(scope, scopeId, relativePath, options)
}

func (self *fileSystemTransaction) ModifyWorkspaceFileByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, modifier func(*models.WorkspaceFile) error, options *store.Option) (*models.WorkspaceFile, error) {
	return self.modifyWorkspaceFile(ctx, scope, scopeId, relativePath, modifier, options)
}

func (self *fileSystemTransaction) DeleteWorkspaceFileByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, options *store.Option) error {
	return self.deleteWorkspaceFile(scope, scopeId, relativePath, options)
}

func (self *fileSystemTransaction) ListWorkspaceFilesByPath(ctx context.Context, scope models.Scope, scopeId string, relativePath string, options *store.Option) ([]*models.WorkspaceFile, error) {
	return self.listWorkspaceFiles(scope, scopeId, relativePath, options)
}

func (self *fileSystemTransaction) SearchWorkspaceFiles(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions store.WorkspaceSearchOptions, options *store.Option) ([]store.WorkspaceFileSearchResult, error) {
	return self.searchWorkspace(ctx, scope, scopeId, query, searchOptions, options)
}
func (self *fileSystemTransaction) createWorkspaceFile(file *models.WorkspaceFile, options *store.Option) (*models.WorkspaceFile, error) {
	if file == nil || file.Scope == nil || file.ScopeID == nil || file.Path == nil {
		return nil, fmt.Errorf("scope, scopeId and path are required")
	}
	absolutePath, err := self.workspaceFilePath(*file.Scope, *file.ScopeID, *file.Path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0755); err != nil {
		return nil, err
	}
	content := []byte{}
	if file.Content != nil {
		content = *file.Content
	}
	if err := atomicfile.WriteFile(absolutePath, content); err != nil {
		return nil, err
	}
	storedFile := *file
	if storedFile.ID == "" {
		storedFile.ID = security.NewULID()
	}
	contentType := storedFile.GetContentType()
	if contentType == "" {
		contentType = "text/plain"
		storedFile.ContentType = &contentType
	}
	modifiedAt := time.Now()
	storedFile.ModifiedAt = &modifiedAt
	if storedFile.CreatedAt == nil {
		storedFile.CreatedAt = &modifiedAt
	}
	return &storedFile, nil
}

func (self *fileSystemTransaction) getWorkspaceFile(scope models.Scope, scopeId string, relativePath string, options *store.Option) (*models.WorkspaceFile, error) {
	absolutePath, err := self.workspaceFilePath(scope, scopeId, relativePath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(absolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	fileInfo, err := os.Stat(absolutePath)
	if err != nil {
		return nil, err
	}
	contentType := "text/plain"
	storedScope := scope
	storedScopeId := scopeId
	storedPath := normalizeRelativePath(relativePath)
	result := &models.WorkspaceFile{
		ID:          security.NewULID(),
		Scope:       &storedScope,
		ScopeID:     &storedScopeId,
		Path:        &storedPath,
		Content:     &content,
		ContentType: &contentType,
	}
	createdAt := fileInfo.ModTime()
	modifiedAt := fileInfo.ModTime()
	result.CreatedAt = &createdAt
	result.ModifiedAt = &modifiedAt
	return result, nil
}

func (self *fileSystemTransaction) modifyWorkspaceFile(ctx context.Context, scope models.Scope, scopeId string, relativePath string, modifier func(*models.WorkspaceFile) error, options *store.Option) (*models.WorkspaceFile, error) {
	file, err := self.GetWorkspaceFileByPath(ctx, scope, scopeId, relativePath, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(file); err != nil {
		return nil, err
	}
	if file.Content == nil {
		emptyContent := []byte{}
		file.Content = &emptyContent
	}
	return self.CreateWorkspaceFile(ctx, file, options)
}

func (self *fileSystemTransaction) deleteWorkspaceFile(scope models.Scope, scopeId string, relativePath string, options *store.Option) error {
	absolutePath, err := self.workspaceFilePath(scope, scopeId, relativePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absolutePath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return trash.Move(absolutePath, self.trashDirectory())
}

func (self *fileSystemTransaction) listWorkspaceFiles(scope models.Scope, scopeId string, relativePath string, options *store.Option) ([]*models.WorkspaceFile, error) {
	rootDirectory, err := self.workspaceRoot(scope, scopeId)
	if err != nil {
		return nil, err
	}
	if relativePath != "" {
		rootDirectory, err = self.workspaceFilePath(scope, scopeId, relativePath)
		if err != nil {
			return nil, err
		}
	}
	fileInformations := make([]*models.WorkspaceFile, 0)
	err = filepath.WalkDir(rootDirectory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		relativeFilePath, relErr := filepath.Rel(self.workspaceDirectory(scope, scopeId), path)
		if relErr != nil {
			return nil
		}
		contentType := "text/plain"
		storedScope := scope
		storedScopeId := scopeId
		normalizedPath := filepath.ToSlash(relativeFilePath)
		modifiedAt := time.Now()
		file := models.WorkspaceFile{
			ID:          security.NewULID(),
			Scope:       &storedScope,
			ScopeID:     &storedScopeId,
			Path:        &normalizedPath,
			Content:     &content,
			ContentType: &contentType,
			CreatedAt:   &modifiedAt,
			ModifiedAt:  &modifiedAt,
		}
		fileInformations = append(fileInformations, &file)
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return applyOffsetLimit(fileInformations, options), nil
}

func (self *fileSystemTransaction) searchWorkspace(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions store.WorkspaceSearchOptions, options *store.Option) ([]store.WorkspaceFileSearchResult, error) {
	pathPrefix := ""
	if searchOptions.PathPrefix != nil {
		pathPrefix = *searchOptions.PathPrefix
	}
	files, err := self.ListWorkspaceFilesByPath(ctx, scope, scopeId, pathPrefix, nil)
	if err != nil {
		return nil, err
	}
	caseSensitive := false
	if searchOptions.CaseSensitive != nil {
		caseSensitive = *searchOptions.CaseSensitive
	}
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}
	results := make([]store.WorkspaceFileSearchResult, 0)
	for _, file := range files {
		if file.Content == nil {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(*file.Content))
		matchedLines := []string{}
		for scanner.Scan() {
			line := scanner.Text()
			haystack := line
			if !caseSensitive {
				haystack = strings.ToLower(haystack)
			}
			if strings.Contains(haystack, needle) {
				matchedLines = append(matchedLines, line)
			}
		}
		if len(matchedLines) == 0 {
			continue
		}
		result := store.WorkspaceFileSearchResult{
			WorkspaceFileID: &file.ID,
			Scope:           file.Scope,
			ScopeID:         file.ScopeID,
			Path:            file.Path,
			MatchedLines:    &matchedLines,
		}
		results = append(results, result)
	}
	if searchOptions.Limit != nil && uint64(len(results)) > *searchOptions.Limit {
		results = results[:*searchOptions.Limit]
	}
	return results, nil
}
