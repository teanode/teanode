package db

import (
	"bufio"
	"bytes"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseWorkspaceFileRecord struct {
	ID          string    `gorm:"column:id;type:varchar(32);primaryKey"`
	Scope       string    `gorm:"column:scope;type:varchar(32);not null"`
	ScopeID     string    `gorm:"column:scope_id;type:varchar(32);not null"`
	Path        string    `gorm:"column:path;type:varchar(512);not null"`
	Content     []byte    `gorm:"column:content;type:bytea;not null"`
	ContentType *string   `gorm:"column:content_type;type:varchar(128)"`
	CreatedAt   time.Time `gorm:"column:created_at;not null"`
	ModifiedAt  time.Time `gorm:"column:modified_at;not null"`
}

func (databaseWorkspaceFileRecord) TableName() string {
	return "workspace_files"
}

func (self *databaseTransaction) CreateWorkspaceFile(file *models.WorkspaceFile, options *store.Option) (*models.WorkspaceFile, error) {
	if file == nil || file.Scope == nil || file.ScopeID == nil || file.Path == nil {
		return nil, store.ErrInvalidOptions
	}
	normalizedPath := normalizeWorkspacePath(*file.Path)
	if isInvalidWorkspacePath(normalizedPath) {
		return nil, store.ErrInvalidOptions
	}
	scope := string(*file.Scope)
	scopeId := strings.TrimSpace(*file.ScopeID)
	if scope == "" || scopeId == "" {
		return nil, store.ErrInvalidOptions
	}
	existingRecord := &databaseWorkspaceFileRecord{}
	getError := self.database.Where("scope = ? AND scope_id = ? AND path = ?", scope, scopeId, normalizedPath).Take(existingRecord).Error

	now := time.Now().UTC()
	record := &databaseWorkspaceFileRecord{
		ID:         strings.TrimSpace(file.ID),
		Scope:      scope,
		ScopeID:    scopeId,
		Path:       normalizedPath,
		Content:    []byte{},
		CreatedAt:  now,
		ModifiedAt: now,
	}
	if file.Content != nil {
		record.Content = *file.Content
	}
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	if existingRecord.ID != "" {
		record.ID = existingRecord.ID
		record.CreatedAt = existingRecord.CreatedAt
	}
	if file.CreatedAt != nil && existingRecord.ID == "" {
		record.CreatedAt = file.CreatedAt.UTC()
	}
	if file.ModifiedAt != nil {
		record.ModifiedAt = file.ModifiedAt.UTC()
	}
	contentType := strings.TrimSpace(valueOrEmptyString(file.ContentType))
	if contentType == "" {
		contentType = "text/plain"
	}
	record.ContentType = &contentType
	if getError != nil && databaseError(getError) != store.ErrNotFound {
		return nil, databaseError(getError)
	}
	saveError := self.database.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "scope"},
			{Name: "scope_id"},
			{Name: "path"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"content", "content_type", "modified_at"}),
	}).Create(record).Error
	if saveError != nil {
		return nil, databaseError(saveError)
	}
	return workspaceRecordToModel(record), nil
}

func (self *databaseTransaction) GetWorkspaceFileByPath(scope models.Scope, scopeId string, path string, options *store.Option) (*models.WorkspaceFile, error) {
	normalizedPath := normalizeWorkspacePath(path)
	record := &databaseWorkspaceFileRecord{}
	getError := self.database.Where("scope = ? AND scope_id = ? AND path = ?", string(scope), strings.TrimSpace(scopeId), normalizedPath).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return workspaceRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyWorkspaceFileByPath(scope models.Scope, scopeId string, path string, modifier func(*models.WorkspaceFile) error, options *store.Option) (*models.WorkspaceFile, error) {
	file, getError := self.GetWorkspaceFileByPath(scope, scopeId, path, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(file); modifierError != nil {
		return nil, modifierError
	}
	file.Scope = ptrto.Value(scope)
	file.ScopeID = ptrto.Value(strings.TrimSpace(scopeId))
	normalizedPath := normalizeWorkspacePath(path)
	file.Path = &normalizedPath
	return self.CreateWorkspaceFile(file, options)
}

func (self *databaseTransaction) DeleteWorkspaceFileByPath(scope models.Scope, scopeId string, path string, options *store.Option) error {
	normalizedPath := normalizeWorkspacePath(path)
	result := self.database.Where("scope = ? AND scope_id = ? AND path = ?", string(scope), strings.TrimSpace(scopeId), normalizedPath).Delete(&databaseWorkspaceFileRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	return nil
}

func (self *databaseTransaction) ListWorkspaceFilesByPath(scope models.Scope, scopeId string, path string, options *store.Option) ([]models.WorkspaceFile, error) {
	normalizedPath := normalizeWorkspacePath(path)
	query := self.database.Model(&databaseWorkspaceFileRecord{}).Where("scope = ? AND scope_id = ?", string(scope), strings.TrimSpace(scopeId))
	if normalizedPath != "" && normalizedPath != "." {
		query = query.Where("path = ? OR path LIKE ?", normalizedPath, normalizedPath+"/%")
	}
	query = query.Order("path ASC")
	query = applyOption(query, options)
	records := make([]databaseWorkspaceFileRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	files := make([]models.WorkspaceFile, 0, len(records))
	for _, record := range records {
		recordCopy := record
		files = append(files, *workspaceRecordToModel(&recordCopy))
	}
	return files, nil
}

func (self *databaseTransaction) SearchWorkspaceFiles(scope models.Scope, scopeId string, query string, searchOptions store.WorkspaceSearchOptions, options *store.Option) ([]store.WorkspaceFileSearchResult, error) {
	listPath := ""
	if searchOptions.PathPrefix != nil {
		listPath = *searchOptions.PathPrefix
	}
	files, listError := self.ListWorkspaceFilesByPath(scope, scopeId, listPath, options)
	if listError != nil {
		return nil, listError
	}
	caseSensitive := searchOptions.CaseSensitive != nil && *searchOptions.CaseSensitive
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
		matchedLines := make([]string, 0)
		for scanner.Scan() {
			line := scanner.Text()
			lineForMatch := line
			if !caseSensitive {
				lineForMatch = strings.ToLower(line)
			}
			if strings.Contains(lineForMatch, needle) {
				matchedLines = append(matchedLines, line)
			}
		}
		if len(matchedLines) == 0 {
			continue
		}
		searchResult := store.WorkspaceFileSearchResult{
			WorkspaceFileID: ptrto.Value(file.ID),
			Scope:           file.Scope,
			ScopeID:         file.ScopeID,
			Path:            file.Path,
			MatchedLines:    &matchedLines,
		}
		if searchOptions.IncludeContent != nil && !*searchOptions.IncludeContent {
			searchResult.MatchedLines = nil
		}
		results = append(results, searchResult)
	}
	if searchOptions.Limit != nil && uint64(len(results)) > *searchOptions.Limit {
		return results[:*searchOptions.Limit], nil
	}
	return results, nil
}

func (self *databaseTransaction) createSeedWorkspaceFiles(scope models.Scope, scopeId string, files []models.WorkspaceFile, options *store.Option) error {
	for _, file := range files {
		fileCopy := file
		fileCopy.Scope = ptrto.Value(scope)
		fileCopy.ScopeID = ptrto.Value(scopeId)
		if _, createError := self.CreateWorkspaceFile(&fileCopy, options); createError != nil {
			return createError
		}
	}
	return nil
}

func workspaceRecordToModel(record *databaseWorkspaceFileRecord) *models.WorkspaceFile {
	scope := models.Scope(record.Scope)
	scopeId := record.ScopeID
	path := record.Path
	content := make([]byte, len(record.Content))
	copy(content, record.Content)
	return &models.WorkspaceFile{
		ID:          record.ID,
		Scope:       &scope,
		ScopeID:     &scopeId,
		Path:        &path,
		Content:     &content,
		ContentType: record.ContentType,
		CreatedAt:   &record.CreatedAt,
		ModifiedAt:  &record.ModifiedAt,
	}
}
