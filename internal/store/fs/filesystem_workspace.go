package fs

import (
	"bufio"
	"bytes"
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

func (self *transaction) CreateWorkspaceFile(file *models.WorkspaceFile, options *store.Option) (*models.WorkspaceFile, error) {
	return self.createWorkspaceFile(file, options)
}

func (self *transaction) GetWorkspaceFileByPath(scope models.Scope, scopeId string, relativePath string, options *store.Option) (*models.WorkspaceFile, error) {
	return self.getWorkspaceFile(scope, scopeId, relativePath, options)
}

func (self *transaction) ModifyWorkspaceFileByPath(scope models.Scope, scopeId string, relativePath string, modifier func(*models.WorkspaceFile) error, options *store.Option) (*models.WorkspaceFile, error) {
	return self.modifyWorkspaceFile(scope, scopeId, relativePath, modifier, options)
}

func (self *transaction) DeleteWorkspaceFileByPath(scope models.Scope, scopeId string, relativePath string, options *store.Option) error {
	return self.deleteWorkspaceFile(scope, scopeId, relativePath, options)
}

func (self *transaction) ListWorkspaceFilesByPath(scope models.Scope, scopeId string, relativePath string, options *store.Option) ([]models.WorkspaceFile, error) {
	return self.listWorkspaceFiles(scope, scopeId, relativePath, options)
}

func (self *transaction) SearchWorkspaceFiles(scope models.Scope, scopeId string, query string, searchOptions store.WorkspaceSearchOptions, options *store.Option) ([]store.WorkspaceFileSearchResult, error) {
	return self.searchWorkspace(scope, scopeId, query, searchOptions, options)
}
func (self *transaction) createWorkspaceFile(file *models.WorkspaceFile, options *store.Option) (*models.WorkspaceFile, error) {
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
	if strings.TrimSpace(storedFile.ID) == "" {
		storedFile.ID = security.NewULID()
	}
	contentType := valueOrEmpty(storedFile.ContentType)
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

func (self *transaction) getWorkspaceFile(scope models.Scope, scopeId string, relativePath string, options *store.Option) (*models.WorkspaceFile, error) {
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

func (self *transaction) modifyWorkspaceFile(scope models.Scope, scopeId string, relativePath string, modifier func(*models.WorkspaceFile) error, options *store.Option) (*models.WorkspaceFile, error) {
	file, err := self.GetWorkspaceFileByPath(scope, scopeId, relativePath, options)
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
	return self.CreateWorkspaceFile(file, options)
}

func (self *transaction) deleteWorkspaceFile(scope models.Scope, scopeId string, relativePath string, options *store.Option) error {
	absolutePath, err := self.workspaceFilePath(scope, scopeId, relativePath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absolutePath); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return trash.Move(absolutePath, self.trashDirectory())
}

func (self *transaction) listWorkspaceFiles(scope models.Scope, scopeId string, relativePath string, options *store.Option) ([]models.WorkspaceFile, error) {
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
	fileInfos := make([]models.WorkspaceFile, 0)
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
		fileInfos = append(fileInfos, models.WorkspaceFile{
			ID:          security.NewULID(),
			Scope:       &storedScope,
			ScopeID:     &storedScopeId,
			Path:        &normalizedPath,
			Content:     &content,
			ContentType: &contentType,
			CreatedAt:   &modifiedAt,
			ModifiedAt:  &modifiedAt,
		})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return applyOffsetLimitFiles(fileInfos, options), nil
}

func (self *transaction) searchWorkspace(scope models.Scope, scopeId string, query string, searchOptions store.WorkspaceSearchOptions, options *store.Option) ([]store.WorkspaceFileSearchResult, error) {
	files, err := self.ListWorkspaceFilesByPath(scope, scopeId, valueOrEmpty(searchOptions.PathPrefix), options)
	if err != nil {
		return nil, err
	}
	caseSensitive := boolValue(searchOptions.CaseSensitive)
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

func applyOffsetLimitFiles(values []models.WorkspaceFile, options *store.Option) []models.WorkspaceFile {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []models.WorkspaceFile{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}
