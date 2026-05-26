package fsstore

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/vmihailenco/msgpack/v5"
)

func (self *fileSystemTransaction) memoryCacheKey(scope models.Scope, scopeId string) string {
	return string(scope) + "\x00" + scopeId
}

func (self *fileSystemTransaction) readMemoryItems(scope models.Scope, scopeId string) ([]*storeMemoryItemRecord, error) {
	key := self.memoryCacheKey(scope, scopeId)
	if self.memoryCache != nil {
		if cached, ok := self.memoryCache[key]; ok {
			return cached, nil
		}
	}

	filePath := self.memoryFilePath(scope, scopeId)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []*storeMemoryItemRecord
	if err := msgpack.Unmarshal(data, &records); err != nil {
		return nil, err
	}

	if self.memoryCache == nil {
		self.memoryCache = make(map[string][]*storeMemoryItemRecord)
	}
	self.memoryCache[key] = records
	return records, nil
}

func (self *fileSystemTransaction) writeMemoryItems(scope models.Scope, scopeId string, records []*storeMemoryItemRecord) error {
	filePath := self.memoryFilePath(scope, scopeId)
	directory := filepath.Dir(filePath)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	data, err := msgpack.Marshal(records)
	if err != nil {
		return err
	}
	if writeErr := atomicfile.WriteFile(filePath, data); writeErr != nil {
		return writeErr
	}

	key := self.memoryCacheKey(scope, scopeId)
	if self.memoryCache == nil {
		self.memoryCache = make(map[string][]*storeMemoryItemRecord)
	}
	self.memoryCache[key] = records
	return nil
}

func (self *fileSystemTransaction) CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *store.Option) (*models.MemoryItem, error) {
	if item == nil || item.Scope == nil || item.ScopeID == nil {
		return nil, store.ErrInvalidOptions
	}
	scope := *item.Scope
	scopeId := *item.ScopeID
	if scope == "" || scopeId == "" {
		return nil, store.ErrInvalidOptions
	}

	now := time.Now()
	itemId := item.ID
	if itemId == "" {
		itemId = security.NewULID()
	}

	record := &storeMemoryItemRecord{
		ID:         itemId,
		Scope:      string(scope),
		ScopeID:    scopeId,
		CreatedAt:  timeutil.Timestamp{Time: now},
		ModifiedAt: timeutil.Timestamp{Time: now},
	}
	if item.Title != nil {
		record.Title = *item.Title
	}
	if item.Content != nil {
		record.Content = *item.Content
	}
	if item.Tags != nil {
		record.Tags = *item.Tags
	}
	if item.EmbeddingProviderModelName != nil {
		record.EmbeddingProviderModelName = *item.EmbeddingProviderModelName
	}
	if item.Embedding != nil {
		record.Embedding = *item.Embedding
	}
	if item.EmbeddedAt != nil {
		record.EmbeddedAt = timeutil.Timestamp{Time: *item.EmbeddedAt}
	}

	records, err := self.readMemoryItems(scope, scopeId)
	if err != nil {
		return nil, err
	}
	records = append(records, record)
	if err := self.writeMemoryItems(scope, scopeId, records); err != nil {
		return nil, err
	}
	return fileSystemMemoryRecordToModel(record), nil
}

func (self *fileSystemTransaction) GetMemoryItem(ctx context.Context, memoryItemId string, options *store.Option) (*models.MemoryItem, error) {
	scope, scopeId, record, err := self.findMemoryItemRecord(memoryItemId)
	if err != nil {
		return nil, err
	}
	_, _ = scope, scopeId
	if !record.ArchivedAt.IsZero() {
		return nil, store.ErrNotFound
	}
	return fileSystemMemoryRecordToModel(record), nil
}

func (self *fileSystemTransaction) ModifyMemoryItem(ctx context.Context, memoryItemId string, modifier func(*models.MemoryItem) error, options *store.Option) (*models.MemoryItem, error) {
	scope, scopeId, record, err := self.findMemoryItemRecord(memoryItemId)
	if err != nil {
		return nil, err
	}
	if !record.ArchivedAt.IsZero() {
		return nil, store.ErrNotFound
	}

	item := fileSystemMemoryRecordToModel(record)
	if modifierError := modifier(item); modifierError != nil {
		return nil, modifierError
	}

	now := time.Now()
	record.ModifiedAt = timeutil.Timestamp{Time: now}
	if item.Title != nil {
		record.Title = *item.Title
	}
	if item.Content != nil {
		record.Content = *item.Content
	}
	if item.Tags != nil {
		record.Tags = *item.Tags
	}
	if item.EmbeddingProviderModelName != nil {
		record.EmbeddingProviderModelName = *item.EmbeddingProviderModelName
	}
	if item.Embedding != nil {
		record.Embedding = *item.Embedding
	}
	if item.EmbeddedAt != nil {
		record.EmbeddedAt = timeutil.Timestamp{Time: *item.EmbeddedAt}
	}

	records, readErr := self.readMemoryItems(scope, scopeId)
	if readErr != nil {
		return nil, readErr
	}
	for i, r := range records {
		if r.ID == memoryItemId {
			records[i] = record
			break
		}
	}
	if err := self.writeMemoryItems(scope, scopeId, records); err != nil {
		return nil, err
	}
	return fileSystemMemoryRecordToModel(record), nil
}

func (self *fileSystemTransaction) DeleteMemoryItem(ctx context.Context, memoryItemId string, options *store.Option) error {
	scope, scopeId, record, err := self.findMemoryItemRecord(memoryItemId)
	if err != nil {
		return err
	}
	if !record.ArchivedAt.IsZero() {
		return store.ErrNotFound
	}
	record.ArchivedAt = timeutil.Timestamp{Time: time.Now()}

	records, readErr := self.readMemoryItems(scope, scopeId)
	if readErr != nil {
		return readErr
	}
	for i, r := range records {
		if r.ID == memoryItemId {
			records[i] = record
			break
		}
	}
	return self.writeMemoryItems(scope, scopeId, records)
}

func (self *fileSystemTransaction) ListMemoryItems(ctx context.Context, scope models.Scope, scopeId string, listOptions store.MemoryItemListOptions, options *store.Option) ([]*models.MemoryItem, error) {
	records, err := self.readMemoryItems(scope, scopeId)
	if err != nil {
		return nil, err
	}

	includeArchived := listOptions.IncludeArchived != nil && *listOptions.IncludeArchived
	var filterTags []string
	if listOptions.Tags != nil {
		filterTags = *listOptions.Tags
	}

	items := make([]*models.MemoryItem, 0, len(records))
	for _, record := range records {
		if !includeArchived && !record.ArchivedAt.IsZero() {
			continue
		}
		if len(filterTags) > 0 && !containsAllTags(record.Tags, filterTags) {
			continue
		}
		items = append(items, fileSystemMemoryRecordToModel(record))
	}

	// Sort by ModifiedAt DESC (newest first).
	sort.Slice(items, func(i, j int) bool {
		if items[i].ModifiedAt == nil || items[j].ModifiedAt == nil {
			return false
		}
		return items[i].ModifiedAt.After(*items[j].ModifiedAt)
	})

	if listOptions.Limit != nil && uint64(len(items)) > *listOptions.Limit {
		items = items[:*listOptions.Limit]
	}
	return items, nil
}

func (self *fileSystemTransaction) SearchMemoryItems(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions store.MemoryItemSearchOptions, options *store.Option) ([]store.MemoryItemSearchResult, error) {
	items, listError := self.ListMemoryItems(ctx, scope, scopeId, store.MemoryItemListOptions{
		IncludeArchived: searchOptions.IncludeArchived,
	}, options)
	if listError != nil {
		return nil, listError
	}

	caseSensitive := searchOptions.CaseSensitive != nil && *searchOptions.CaseSensitive
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(needle)
	}

	results := make([]store.MemoryItemSearchResult, 0)
	for _, item := range items {
		matchedLines := make([]string, 0)

		// Search title.
		if item.Title != nil {
			titleLine := *item.Title
			titleForMatch := titleLine
			if !caseSensitive {
				titleForMatch = strings.ToLower(titleForMatch)
			}
			if strings.Contains(titleForMatch, needle) {
				matchedLines = append(matchedLines, titleLine)
			}
		}

		// Search content line by line.
		if item.Content != nil {
			scanner := bufio.NewScanner(strings.NewReader(*item.Content))
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
		}

		if len(matchedLines) == 0 {
			continue
		}

		searchResult := store.MemoryItemSearchResult{
			MemoryItemID: ptrto.Value(item.ID),
			Scope:        item.Scope,
			ScopeID:      item.ScopeID,
			Title:        item.Title,
			Tags:         item.Tags,
			MatchedLines: &matchedLines,
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

// findMemoryItemRecord searches for a memory item by ID across all scope directories.
func (self *fileSystemTransaction) findMemoryItemRecord(itemId string) (models.Scope, string, *storeMemoryItemRecord, error) {
	for _, scope := range []models.Scope{models.ScopeAgent, models.ScopeUser, models.ScopeProject} {
		var scopeDir string
		switch scope {
		case models.ScopeAgent:
			scopeDir = self.agentsDirectory()
		case models.ScopeUser:
			scopeDir = self.usersDirectory()
		case models.ScopeProject:
			scopeDir = self.projectsDirectory()
		}
		entries, err := os.ReadDir(scopeDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			scopeId := entry.Name()
			records, readErr := self.readMemoryItems(scope, scopeId)
			if readErr != nil {
				continue
			}
			for _, record := range records {
				if record.ID == itemId {
					return scope, scopeId, record, nil
				}
			}
		}
	}
	return "", "", nil, store.ErrNotFound
}

func fileSystemMemoryRecordToModel(record *storeMemoryItemRecord) *models.MemoryItem {
	scope := models.Scope(record.Scope)
	scopeId := record.ScopeID
	content := record.Content

	item := &models.MemoryItem{
		ID:      record.ID,
		Scope:   &scope,
		ScopeID: &scopeId,
		Content: &content,
	}
	if record.Title != "" {
		title := record.Title
		item.Title = &title
	}
	if len(record.Tags) > 0 {
		tags := make([]string, len(record.Tags))
		copy(tags, record.Tags)
		item.Tags = &tags
	}
	if !record.CreatedAt.IsZero() {
		createdAt := record.CreatedAt.Time
		item.CreatedAt = &createdAt
	}
	if !record.ModifiedAt.IsZero() {
		modifiedAt := record.ModifiedAt.Time
		item.ModifiedAt = &modifiedAt
	}
	if !record.ArchivedAt.IsZero() {
		archivedAt := record.ArchivedAt.Time
		item.ArchivedAt = &archivedAt
	}
	if record.EmbeddingProviderModelName != "" {
		embeddingProviderModelName := record.EmbeddingProviderModelName
		item.EmbeddingProviderModelName = &embeddingProviderModelName
	}
	if len(record.Embedding) > 0 {
		embedding := make([]float64, len(record.Embedding))
		copy(embedding, record.Embedding)
		item.Embedding = &embedding
	}
	if !record.EmbeddedAt.IsZero() {
		embeddedAt := record.EmbeddedAt.Time
		item.EmbeddedAt = &embeddedAt
	}
	return item
}

func containsAllTags(haystack []string, needles []string) bool {
	tagSet := make(map[string]struct{}, len(haystack))
	for _, tag := range haystack {
		tagSet[tag] = struct{}{}
	}
	for _, needle := range needles {
		if _, ok := tagSet[needle]; !ok {
			return false
		}
	}
	return true
}
