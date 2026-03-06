package dbstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/valueor"
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
	intervalType := timeutil.IntervalType(record.IntervalType)
	return &models.Usage{
		UserID:              ptrto.Value(record.UserID),
		ProviderName:        ptrto.Value(record.ProviderName),
		ModelName:           ptrto.Value(record.ModelName),
		IntervalType:        &intervalType,
		StartedAt:           ptrto.Value(record.StartedAt),
		PromptTokens:        ptrto.Value(uint64(record.PromptTokens)),
		CompletionTokens:    ptrto.Value(uint64(record.CompletionTokens)),
		CacheCreationTokens: ptrto.Value(uint64(record.CacheCreationTokens)),
		CacheReadTokens:     ptrto.Value(uint64(record.CacheReadTokens)),
		TotalTokens:         ptrto.Value(uint64(record.TotalTokens)),
		RequestCount:        ptrto.Value(uint64(record.RequestCount)),
	}
}

// --- operations ---

// Retention limits per interval type, matching fsstore.
var maxEntriesPerInterval = map[timeutil.IntervalType]int{
	timeutil.IntervalTypeHour:  168, // 7 days
	timeutil.IntervalTypeDay:   90,  // 90 days
	timeutil.IntervalTypeWeek:  52,  // 1 year
	timeutil.IntervalTypeMonth: 24,  // 2 years
	timeutil.IntervalTypeYear:  10,  // 10 years
}

func (self *databaseTransaction) AccumulateUsage(ctx context.Context, usage *models.Usage, options *store.Option) error {
	if usage == nil {
		return store.ErrInvalidOptions
	}
	now := time.Now()
	local := now.In(time.Local)
	for _, intervalType := range []timeutil.IntervalType{timeutil.IntervalTypeHour, timeutil.IntervalTypeDay, timeutil.IntervalTypeWeek, timeutil.IntervalTypeMonth, timeutil.IntervalTypeYear} {
		truncated := timeutil.TruncateToInterval(local, intervalType)
		startedAt := time.Date(truncated.Year(), truncated.Month(), truncated.Day(),
			truncated.Hour(), truncated.Minute(), truncated.Second(), 0, time.UTC)
		inserted, err := self.accumulateUsageBucket(usage, intervalType, startedAt)
		if err != nil {
			return err
		}
		if inserted {
			if err := self.evictUsageBucket(usage, intervalType); err != nil {
				return err
			}
		}
	}
	return nil
}

func (self *databaseTransaction) accumulateUsageBucket(usage *models.Usage, intervalType timeutil.IntervalType, startedAt time.Time) (bool, error) {
	var inserted bool
	err := self.database.Raw(
		`INSERT INTO usages (user_id, provider_name, model_name, interval_type, started_at, prompt_tokens, completion_tokens, cache_creation_tokens, cache_read_tokens, total_tokens, request_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (user_id, provider_name, model_name, interval_type, started_at)
		 DO UPDATE SET
		     prompt_tokens         = usages.prompt_tokens + EXCLUDED.prompt_tokens,
		     completion_tokens     = usages.completion_tokens + EXCLUDED.completion_tokens,
		     cache_creation_tokens = usages.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
		     cache_read_tokens     = usages.cache_read_tokens + EXCLUDED.cache_read_tokens,
		     total_tokens          = usages.total_tokens + EXCLUDED.total_tokens,
		     request_count         = usages.request_count + EXCLUDED.request_count
		 RETURNING (xmax = 0)`,
		valueor.Zero(usage.UserID), valueor.Zero(usage.ProviderName), valueor.Zero(usage.ModelName), string(intervalType), startedAt,
		int64(valueor.Zero(usage.PromptTokens)), int64(valueor.Zero(usage.CompletionTokens)), int64(valueor.Zero(usage.CacheCreationTokens)),
		int64(valueor.Zero(usage.CacheReadTokens)), int64(valueor.Zero(usage.TotalTokens)), int64(valueor.Zero(usage.RequestCount)),
	).Scan(&inserted).Error
	if err != nil {
		return false, databaseError(err)
	}
	return inserted, nil
}

// evictUsageBucket deletes old entries beyond the retention limit for one interval type.
func (self *databaseTransaction) evictUsageBucket(usage *models.Usage, intervalType timeutil.IntervalType) error {
	maxEntries := maxEntriesPerInterval[intervalType]
	result := self.database.Exec(
		`DELETE FROM usages
		 WHERE user_id = ? AND provider_name = ? AND model_name = ? AND interval_type = ?
		   AND started_at NOT IN (
		     SELECT started_at FROM usages
		     WHERE user_id = ? AND provider_name = ? AND model_name = ? AND interval_type = ?
		     ORDER BY started_at DESC
		     LIMIT ?
		   )`,
		valueor.Zero(usage.UserID), valueor.Zero(usage.ProviderName), valueor.Zero(usage.ModelName), string(intervalType),
		valueor.Zero(usage.UserID), valueor.Zero(usage.ProviderName), valueor.Zero(usage.ModelName), string(intervalType),
		maxEntries,
	)
	if result.Error != nil {
		return databaseError(result.Error)
	}
	return nil
}

func (self *databaseTransaction) ListUsages(ctx context.Context, listOptions store.UsageListOptions, options *store.Option) ([]*models.Usage, error) {
	query := self.database.Table("usages").
		Where("interval_type = ?", string(listOptions.IntervalType)).
		Where("started_at >= ?", listOptions.StartedAt).
		Where("started_at < ?", listOptions.EndedAt)

	if listOptions.UserID != nil {
		query = query.Where("user_id = ?", *listOptions.UserID)
	}
	if listOptions.ProviderName != nil {
		query = query.Where("provider_name = ?", *listOptions.ProviderName)
	}
	if listOptions.ModelName != nil {
		query = query.Where("model_name = ?", *listOptions.ModelName)
	}

	query = query.Order("started_at ASC")
	query = applyOption(query, options)

	var records []databaseUsageRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, databaseError(err)
	}

	entries := make([]*models.Usage, 0, len(records))
	for _, record := range records {
		entries = append(entries, usageRecordToModel(&record))
	}
	return entries, nil
}
