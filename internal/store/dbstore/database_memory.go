package dbstore

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseMemoryItemRecord struct {
	ID         string     `gorm:"column:id;type:varchar(32);primaryKey"`
	Scope      string     `gorm:"column:scope;type:varchar(32);not null"`
	ScopeID    string     `gorm:"column:scope_id;type:varchar(32);not null"`
	Title      *string    `gorm:"column:title;type:text"`
	Content    string     `gorm:"column:content;type:text;not null"`
	Tags       *string    `gorm:"column:tags;type:jsonb"`
	ArchivedAt *time.Time `gorm:"column:archived_at"`
	CreatedAt  time.Time  `gorm:"column:created_at;not null"`
	ModifiedAt time.Time  `gorm:"column:modified_at;not null"`
}

func (databaseMemoryItemRecord) TableName() string {
	return "memory_items"
}

func (self *databaseTransaction) CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *store.Option) (*models.MemoryItem, error) {
	if item == nil || item.Scope == nil || item.ScopeID == nil {
		return nil, store.ErrInvalidOptions
	}
	scope := string(*item.Scope)
	scopeId := *item.ScopeID
	if scope == "" || scopeId == "" {
		return nil, store.ErrInvalidOptions
	}

	now := *ptrto.TimeNowInLocal()
	record := &databaseMemoryItemRecord{
		ID:         item.ID,
		Scope:      scope,
		ScopeID:    scopeId,
		Title:      item.Title,
		CreatedAt:  now,
		ModifiedAt: now,
	}
	if item.Content != nil {
		record.Content = *item.Content
	}
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	if item.Tags != nil {
		tagsJSON, err := json.Marshal(*item.Tags)
		if err != nil {
			return nil, err
		}
		tagsStr := string(tagsJSON)
		record.Tags = &tagsStr
	}

	createError := self.database.Create(record).Error
	if createError != nil {
		return nil, databaseError(createError)
	}
	return memoryRecordToModel(record), nil
}

func (self *databaseTransaction) GetMemoryItem(ctx context.Context, memoryItemId string, options *store.Option) (*models.MemoryItem, error) {
	record := &databaseMemoryItemRecord{}
	getError := self.database.Where("id = ? AND archived_at IS NULL", memoryItemId).Take(record).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	return memoryRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyMemoryItem(ctx context.Context, memoryItemId string, modifier func(*models.MemoryItem) error, options *store.Option) (*models.MemoryItem, error) {
	item, getError := self.GetMemoryItem(ctx, memoryItemId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(item); modifierError != nil {
		return nil, modifierError
	}

	now := *ptrto.TimeNowInLocal()
	record := &databaseMemoryItemRecord{
		ID:         item.ID,
		Scope:      string(*item.Scope),
		ScopeID:    *item.ScopeID,
		Title:      item.Title,
		CreatedAt:  *item.CreatedAt,
		ModifiedAt: now,
	}
	if item.Content != nil {
		record.Content = *item.Content
	}
	if item.Tags != nil {
		tagsJSON, err := json.Marshal(*item.Tags)
		if err != nil {
			return nil, err
		}
		tagsStr := string(tagsJSON)
		record.Tags = &tagsStr
	}

	saveError := self.database.Save(record).Error
	if saveError != nil {
		return nil, databaseError(saveError)
	}
	return memoryRecordToModel(record), nil
}

func (self *databaseTransaction) DeleteMemoryItem(ctx context.Context, memoryItemId string, options *store.Option) error {
	now := *ptrto.TimeNowInLocal()
	result := self.database.Model(&databaseMemoryItemRecord{}).Where("id = ? AND archived_at IS NULL", memoryItemId).Update("archived_at", now)
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (self *databaseTransaction) ListMemoryItems(ctx context.Context, scope models.Scope, scopeId string, listOptions store.MemoryItemListOptions, options *store.Option) ([]*models.MemoryItem, error) {
	query := self.database.Model(&databaseMemoryItemRecord{}).Where("scope = ? AND scope_id = ?", string(scope), scopeId)

	includeArchived := listOptions.IncludeArchived != nil && *listOptions.IncludeArchived
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}

	if listOptions.Tags != nil && len(*listOptions.Tags) > 0 {
		tagsJSON, err := json.Marshal(*listOptions.Tags)
		if err != nil {
			return nil, err
		}
		query = query.Where("tags @> ?", string(tagsJSON))
	}

	query = query.Order("modified_at DESC")

	if listOptions.Limit != nil {
		query = query.Limit(int(*listOptions.Limit))
	}
	query = applyOption(query, options)

	records := make([]databaseMemoryItemRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}

	items := make([]*models.MemoryItem, 0, len(records))
	for _, record := range records {
		recordCopy := record
		items = append(items, memoryRecordToModel(&recordCopy))
	}
	return items, nil
}

func (self *databaseTransaction) SearchMemoryItems(ctx context.Context, scope models.Scope, scopeId string, query string, searchOptions store.MemoryItemSearchOptions, options *store.Option) ([]store.MemoryItemSearchResult, error) {
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

func memoryRecordToModel(record *databaseMemoryItemRecord) *models.MemoryItem {
	scope := models.Scope(record.Scope)
	scopeId := record.ScopeID
	content := record.Content

	item := &models.MemoryItem{
		ID:         record.ID,
		Scope:      &scope,
		ScopeID:    &scopeId,
		Title:      record.Title,
		Content:    &content,
		CreatedAt:  &record.CreatedAt,
		ModifiedAt: &record.ModifiedAt,
		ArchivedAt: record.ArchivedAt,
	}

	if record.Tags != nil {
		var tags []string
		if err := json.Unmarshal([]byte(*record.Tags), &tags); err == nil {
			item.Tags = &tags
		}
	}

	return item
}
