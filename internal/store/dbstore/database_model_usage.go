package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/security"
)

// --- records ---

type databaseModelUsageEventRecord struct {
	ID                  string    `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID              string    `gorm:"column:user_id;type:varchar(32);not null"`
	ConversationID      string    `gorm:"column:conversation_id;type:varchar(32);not null"`
	MessageID           string    `gorm:"column:message_id;type:varchar(32);not null"`
	RunID               string    `gorm:"column:run_id;type:varchar(32);not null"`
	ProviderName        string    `gorm:"column:provider_name;type:varchar(128);not null"`
	ModelName           string    `gorm:"column:model_name;type:varchar(128);not null"`
	PromptTokens        int64     `gorm:"column:prompt_tokens;not null;default:0"`
	CompletionTokens    int64     `gorm:"column:completion_tokens;not null;default:0"`
	CacheCreationTokens int64     `gorm:"column:cache_creation_tokens;not null;default:0"`
	CacheReadTokens     int64     `gorm:"column:cache_read_tokens;not null;default:0"`
	TotalTokens         int64     `gorm:"column:total_tokens;not null;default:0"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
}

func (databaseModelUsageEventRecord) TableName() string {
	return "model_usage_events"
}

type databaseModelUsageStatEntryRecord struct {
	UserID              string    `gorm:"column:user_id;type:varchar(32);not null"`
	ProviderName        string    `gorm:"column:provider_name;type:varchar(128);not null"`
	ModelName           string    `gorm:"column:model_name;type:varchar(128);not null"`
	IntervalType        string    `gorm:"column:interval_type;type:text;not null"`
	StartedAt           time.Time `gorm:"column:started_at;not null"`
	PromptTokens        int64     `gorm:"column:prompt_tokens;not null;default:0"`
	CompletionTokens    int64     `gorm:"column:completion_tokens;not null;default:0"`
	CacheCreationTokens int64     `gorm:"column:cache_creation_tokens;not null;default:0"`
	CacheReadTokens     int64     `gorm:"column:cache_read_tokens;not null;default:0"`
	TotalTokens         int64     `gorm:"column:total_tokens;not null;default:0"`
	RequestCount        int64     `gorm:"column:request_count;not null;default:0"`
}

func (databaseModelUsageStatEntryRecord) TableName() string {
	return "model_usage_stat_entries"
}

// --- conversions ---

func modelToUsageEventRecord(event *models.ModelUsageEvent) *databaseModelUsageEventRecord {
	return &databaseModelUsageEventRecord{
		ID:                  event.ID,
		UserID:              event.UserID,
		ConversationID:      event.ConversationID,
		MessageID:           event.MessageID,
		RunID:               event.RunID,
		ProviderName:        event.ProviderName,
		ModelName:           event.ModelName,
		PromptTokens:        int64(event.PromptTokens),
		CompletionTokens:    int64(event.CompletionTokens),
		CacheCreationTokens: int64(event.CacheCreationTokens),
		CacheReadTokens:     int64(event.CacheReadTokens),
		TotalTokens:         int64(event.TotalTokens),
		CreatedAt:           event.CreatedAt,
	}
}

func usageEventRecordToModel(record *databaseModelUsageEventRecord) *models.ModelUsageEvent {
	return &models.ModelUsageEvent{
		ID:                  record.ID,
		UserID:              record.UserID,
		ConversationID:      record.ConversationID,
		MessageID:           record.MessageID,
		RunID:               record.RunID,
		ProviderName:        record.ProviderName,
		ModelName:           record.ModelName,
		PromptTokens:        uint64(record.PromptTokens),
		CompletionTokens:    uint64(record.CompletionTokens),
		CacheCreationTokens: uint64(record.CacheCreationTokens),
		CacheReadTokens:     uint64(record.CacheReadTokens),
		TotalTokens:         uint64(record.TotalTokens),
		CreatedAt:           record.CreatedAt,
	}
}

func usageStatEntryRecordToModel(record *databaseModelUsageStatEntryRecord) *models.ModelUsageStatEntry {
	return &models.ModelUsageStatEntry{
		UserID:              record.UserID,
		ProviderName:        record.ProviderName,
		ModelName:           record.ModelName,
		IntervalType:        models.IntervalType(record.IntervalType),
		StartedAt:           record.StartedAt,
		PromptTokens:        uint64(record.PromptTokens),
		CompletionTokens:    uint64(record.CompletionTokens),
		CacheCreationTokens: uint64(record.CacheCreationTokens),
		CacheReadTokens:     uint64(record.CacheReadTokens),
		TotalTokens:         uint64(record.TotalTokens),
		RequestCount:        uint64(record.RequestCount),
	}
}

// --- operations ---

func (self *databaseTransaction) CreateModelUsageEvent(ctx context.Context, event *models.ModelUsageEvent, options *store.Option) error {
	if event == nil {
		return store.ErrInvalidOptions
	}
	record := modelToUsageEventRecord(event)
	if record.ID == "" {
		record.ID = security.NewULID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	// INSERT ... ON CONFLICT (message_id) DO NOTHING for idempotent retries.
	result := self.database.Exec(
		`INSERT INTO model_usage_events (id, user_id, conversation_id, message_id, run_id, provider_name, model_name, prompt_tokens, completion_tokens, cache_creation_tokens, cache_read_tokens, total_tokens, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (message_id) DO NOTHING`,
		record.ID, record.UserID, record.ConversationID, record.MessageID, record.RunID,
		record.ProviderName, record.ModelName,
		record.PromptTokens, record.CompletionTokens, record.CacheCreationTokens,
		record.CacheReadTokens, record.TotalTokens, record.CreatedAt,
	)
	if result.Error != nil {
		return databaseError(result.Error)
	}
	return nil
}

func (self *databaseTransaction) UpsertModelUsageStatEntry(ctx context.Context, event *models.ModelUsageEvent, intervalType models.IntervalType, startedAt time.Time, options *store.Option) error {
	if event == nil {
		return store.ErrInvalidOptions
	}
	result := self.database.Exec(
		`INSERT INTO model_usage_stat_entries (user_id, provider_name, model_name, interval_type, started_at, prompt_tokens, completion_tokens, cache_creation_tokens, cache_read_tokens, total_tokens, request_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		 ON CONFLICT (user_id, provider_name, model_name, interval_type, started_at)
		 DO UPDATE SET
		     prompt_tokens         = model_usage_stat_entries.prompt_tokens + EXCLUDED.prompt_tokens,
		     completion_tokens     = model_usage_stat_entries.completion_tokens + EXCLUDED.completion_tokens,
		     cache_creation_tokens = model_usage_stat_entries.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
		     cache_read_tokens     = model_usage_stat_entries.cache_read_tokens + EXCLUDED.cache_read_tokens,
		     total_tokens          = model_usage_stat_entries.total_tokens + EXCLUDED.total_tokens,
		     request_count         = model_usage_stat_entries.request_count + EXCLUDED.request_count`,
		event.UserID, event.ProviderName, event.ModelName, string(intervalType), startedAt,
		int64(event.PromptTokens), int64(event.CompletionTokens), int64(event.CacheCreationTokens),
		int64(event.CacheReadTokens), int64(event.TotalTokens),
	)
	if result.Error != nil {
		return databaseError(result.Error)
	}
	return nil
}

func (self *databaseTransaction) QueryModelUsageStatEntries(ctx context.Context, query store.ModelUsageStatQuery, options *store.Option) ([]*models.ModelUsageStatEntry, error) {
	db := self.database.Table("model_usage_stat_entries").
		Where("user_id = ?", query.UserID).
		Where("interval_type = ?", string(query.IntervalType)).
		Where("started_at >= ?", query.StartedAt).
		Where("started_at < ?", query.EndedAt)

	if query.ProviderName != nil {
		db = db.Where("provider_name = ?", *query.ProviderName)
	}
	if query.ModelName != nil {
		db = db.Where("model_name = ?", *query.ModelName)
	}

	db = db.Order("started_at ASC")
	db = applyOption(db, options)

	var records []databaseModelUsageStatEntryRecord
	if err := db.Find(&records).Error; err != nil {
		return nil, databaseError(err)
	}

	entries := make([]*models.ModelUsageStatEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, usageStatEntryRecordToModel(&record))
	}
	return entries, nil
}
