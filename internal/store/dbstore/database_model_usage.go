package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

// --- records ---

type databaseUsageRecord struct {
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

func (databaseUsageRecord) TableName() string {
	return "usages"
}

// --- conversions ---

func usageRecordToModel(record *databaseUsageRecord) *models.Usage {
	return &models.Usage{
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

func (self *databaseTransaction) UpsertUsage(ctx context.Context, usage *models.Usage, options *store.Option) error {
	if usage == nil {
		return store.ErrInvalidOptions
	}
	result := self.database.Exec(
		`INSERT INTO usages (user_id, provider_name, model_name, interval_type, started_at, prompt_tokens, completion_tokens, cache_creation_tokens, cache_read_tokens, total_tokens, request_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, provider_name, model_name, interval_type, started_at)
		 DO UPDATE SET
		     prompt_tokens         = usages.prompt_tokens + EXCLUDED.prompt_tokens,
		     completion_tokens     = usages.completion_tokens + EXCLUDED.completion_tokens,
		     cache_creation_tokens = usages.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
		     cache_read_tokens     = usages.cache_read_tokens + EXCLUDED.cache_read_tokens,
		     total_tokens          = usages.total_tokens + EXCLUDED.total_tokens,
		     request_count         = usages.request_count + EXCLUDED.request_count`,
		usage.UserID, usage.ProviderName, usage.ModelName, string(usage.IntervalType), usage.StartedAt,
		int64(usage.PromptTokens), int64(usage.CompletionTokens), int64(usage.CacheCreationTokens),
		int64(usage.CacheReadTokens), int64(usage.TotalTokens), int64(usage.RequestCount),
	)
	if result.Error != nil {
		return databaseError(result.Error)
	}
	return nil
}

func (self *databaseTransaction) QueryUsages(ctx context.Context, query store.UsageQuery, options *store.Option) ([]*models.Usage, error) {
	db := self.database.Table("usages").
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

	var records []databaseUsageRecord
	if err := db.Find(&records).Error; err != nil {
		return nil, databaseError(err)
	}

	entries := make([]*models.Usage, 0, len(records))
	for _, record := range records {
		entries = append(entries, usageRecordToModel(&record))
	}
	return entries, nil
}
