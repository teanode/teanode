package fsstore

import (
	"bufio"
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"gopkg.in/yaml.v3"
)

func (self *fileSystemTransaction) CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *store.Option) (*models.MemoryItem, error) {
	if item == nil || item.Scope == nil || item.ScopeID == nil {
		return nil, store.ErrInvalidOptions
	}
	scope := *item.Scope
	scopeID := *item.ScopeID
	if scope == "" || scopeID == "" {
		return nil, store.ErrInvalidOptions
	}

	now := time.Now()
	itemID := item.ID
	if itemID == "" {
		itemID = security.NewULID()
	}

	content := ""
	if item.Content != nil {
		content = *item.Content
	}

	record := storeMemoryItemRecord{
		ID:         itemID,
		Scope:      string(scope),
		ScopeID:    scopeID,
		Content:    content,
		CreatedAt:  timeutil.Timestamp{Time: now},
		ModifiedAt: timeutil.Timestamp{Time: now},
	}
	if item.Title != nil {
		record.Title = *item.Title
	}
	if item.Tags != nil {
		record.Tags = *item.Tags
	}

	filePath := self.memoryItemFilePath(scope, scopeID, itemID)
	if err := writeYAMLFile(filePath, &record); err != nil {
		return nil, err
	}
	return fsMemoryRecordToModel(&record), nil
}

func (self *fileSystemTransaction) GetMemoryItem(ctx context.Context, memoryItemID string, options *store.Option) (*models.MemoryItem, error) {
	filePath, err := self.findMemoryItemFilePath(memoryItemID)
	if err != nil {
		return nil, err
	}
	record, err := readMemoryItemFile(filePath)
	if err != nil {
		return nil, err
	}
	if !record.ArchivedAt.IsZero() {
		return nil, store.ErrNotFound
	}
	return fsMemoryRecordToModel(record), nil
}

func (self *fileSystemTransaction) ModifyMemoryItem(ctx context.Context, memoryItemID string, modifier func(*models.MemoryItem) error, options *store.Option) (*models.MemoryItem, error) {
	item, getError := self.GetMemoryItem(ctx, memoryItemID, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(item); modifierError != nil {
		return nil, modifierError
	}

	now := time.Now()
	record := storeMemoryItemRecord{
		ID:         item.ID,
		Scope:      string(*item.Scope),
		ScopeID:    *item.ScopeID,
		CreatedAt:  timeutil.Timestamp{Time: *item.CreatedAt},
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

	filePath := self.memoryItemFilePath(*item.Scope, *item.ScopeID, item.ID)
	if err := writeYAMLFile(filePath, &record); err != nil {
		return nil, err
	}
	return fsMemoryRecordToModel(&record), nil
}

func (self *fileSystemTransaction) DeleteMemoryItem(ctx context.Context, memoryItemID string, options *store.Option) error {
	filePath, err := self.findMemoryItemFilePath(memoryItemID)
	if err != nil {
		return err
	}
	record, err := readMemoryItemFile(filePath)
	if err != nil {
		return err
	}
	if !record.ArchivedAt.IsZero() {
		return store.ErrNotFound
	}
	record.ArchivedAt = timeutil.Timestamp{Time: time.Now()}
	return writeYAMLFile(filePath, record)
}

func (self *fileSystemTransaction) ListMemoryItems(ctx context.Context, scope models.Scope, scopeID string, listOptions store.MemoryItemListOptions, options *store.Option) ([]*models.MemoryItem, error) {
	directory := self.memoryItemDirectory(scope, scopeID)
	entries, readError := os.ReadDir(directory)
	if os.IsNotExist(readError) {
		return []*models.MemoryItem{}, nil
	}
	if readError != nil {
		return nil, readError
	}

	includeArchived := listOptions.IncludeArchived != nil && *listOptions.IncludeArchived
	var filterTags []string
	if listOptions.Tags != nil {
		filterTags = *listOptions.Tags
	}

	items := make([]*models.MemoryItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		filePath := directory + "/" + entry.Name()
		record, err := readMemoryItemFile(filePath)
		if err != nil {
			continue
		}
		if !includeArchived && !record.ArchivedAt.IsZero() {
			continue
		}
		if len(filterTags) > 0 && !containsAllTags(record.Tags, filterTags) {
			continue
		}
		items = append(items, fsMemoryRecordToModel(record))
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

func (self *fileSystemTransaction) SearchMemoryItems(ctx context.Context, scope models.Scope, scopeID string, query string, searchOptions store.MemoryItemSearchOptions, options *store.Option) ([]store.MemoryItemSearchResult, error) {
	items, listError := self.ListMemoryItems(ctx, scope, scopeID, store.MemoryItemListOptions{
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

// findMemoryItemFilePath searches for a memory item file by ID across all scope directories.
func (self *fileSystemTransaction) findMemoryItemFilePath(itemID string) (string, error) {
	filename := itemID + ".yaml"

	// Check agents.
	agentPattern := self.agentsDirectory() + "/*/memory/" + filename
	if matches, _ := doubleStarGlob(agentPattern); len(matches) > 0 {
		return matches[0], nil
	}

	// Check users.
	userPattern := self.usersDirectory() + "/*/memory/" + filename
	if matches, _ := doubleStarGlob(userPattern); len(matches) > 0 {
		return matches[0], nil
	}

	// Check projects.
	projectPattern := self.projectsDirectory() + "/*/memory/" + filename
	if matches, _ := doubleStarGlob(projectPattern); len(matches) > 0 {
		return matches[0], nil
	}

	return "", store.ErrNotFound
}

func readMemoryItemFile(filePath string) (*storeMemoryItemRecord, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	var record storeMemoryItemRecord
	if err := yaml.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func fsMemoryRecordToModel(record *storeMemoryItemRecord) *models.MemoryItem {
	scope := models.Scope(record.Scope)
	scopeID := record.ScopeID
	content := record.Content

	item := &models.MemoryItem{
		ID:      record.ID,
		Scope:   &scope,
		ScopeID: &scopeID,
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
		t := record.CreatedAt.Time
		item.CreatedAt = &t
	}
	if !record.ModifiedAt.IsZero() {
		t := record.ModifiedAt.Time
		item.ModifiedAt = &t
	}
	if !record.ArchivedAt.IsZero() {
		t := record.ArchivedAt.Time
		item.ArchivedAt = &t
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
