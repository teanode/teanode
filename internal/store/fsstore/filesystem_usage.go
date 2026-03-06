package fsstore

import (
	"context"
	"os"
	"sort"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/valueor"
	"github.com/vmihailenco/msgpack/v5"
)

// Default retention limits per (userId, providerName, modelName, intervalType).
const (
	defaultMaxHourEntries  = 168 // 7 days
	defaultMaxDayEntries   = 90  // 90 days
	defaultMaxWeekEntries  = 52  // 1 year
	defaultMaxMonthEntries = 24  // 2 years
	defaultMaxYearEntries  = 10  // 10 years
)

func (self *fileSystemTransaction) usagesFilename() string {
	return self.store.dataDirectory + "/usages.msgpack"
}

func (self *fileSystemTransaction) readUsages() ([]*models.Usage, error) {
	data, err := os.ReadFile(self.usagesFilename())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var usages []*models.Usage
	if err := msgpack.Unmarshal(data, &usages); err != nil {
		return nil, err
	}
	return usages, nil
}

func (self *fileSystemTransaction) writeUsages(usages []*models.Usage) error {
	data, err := msgpack.Marshal(usages)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(self.usagesFilename(), data)
}

// evictUsages trims entries per (userId, providerName, modelName, intervalType)
// to the configured maximum, keeping the most recent entries.
func evictUsages(usages []*models.Usage) []*models.Usage {
	type groupKey struct {
		UserID       string
		ProviderName string
		ModelName    string
		IntervalType timeutil.IntervalType
	}
	groups := map[groupKey][]*models.Usage{}
	for _, entry := range usages {
		key := groupKey{valueor.Zero(entry.UserID), valueor.Zero(entry.ProviderName), valueor.Zero(entry.ModelName), valueor.Zero(entry.IntervalType)}
		groups[key] = append(groups[key], entry)
	}
	var result []*models.Usage
	for key, entries := range groups {
		var maxEntries int
		switch key.IntervalType {
		case timeutil.IntervalTypeHour:
			maxEntries = defaultMaxHourEntries
		case timeutil.IntervalTypeDay:
			maxEntries = defaultMaxDayEntries
		case timeutil.IntervalTypeWeek:
			maxEntries = defaultMaxWeekEntries
		case timeutil.IntervalTypeMonth:
			maxEntries = defaultMaxMonthEntries
		case timeutil.IntervalTypeYear:
			maxEntries = defaultMaxYearEntries
		default:
			maxEntries = defaultMaxDayEntries
		}
		if len(entries) > maxEntries {
			sort.Slice(entries, func(i, j int) bool {
				return valueor.Zero(entries[i].StartedAt).Before(valueor.Zero(entries[j].StartedAt))
			})
			entries = entries[len(entries)-maxEntries:]
		}
		result = append(result, entries...)
	}
	return result
}

func (self *fileSystemTransaction) AccumulateUsage(ctx context.Context, usage *models.Usage, options *store.Option) error {
	if usage == nil {
		return store.ErrInvalidOptions
	}
	usages, err := self.readUsages()
	if err != nil {
		return err
	}
	now := time.Now()
	local := now.In(time.Local)
	for _, intervalType := range []timeutil.IntervalType{timeutil.IntervalTypeHour, timeutil.IntervalTypeDay, timeutil.IntervalTypeWeek, timeutil.IntervalTypeMonth, timeutil.IntervalTypeYear} {
		startedAt := timeutil.TruncateToInterval(local, intervalType)
		usages = accumulateUsageBucket(usages, usage, intervalType, startedAt)
	}
	usages = evictUsages(usages)
	return self.writeUsages(usages)
}

func accumulateUsageBucket(usages []*models.Usage, usage *models.Usage, intervalType timeutil.IntervalType, startedAt time.Time) []*models.Usage {
	for _, existing := range usages {
		if valueor.Zero(existing.UserID) == valueor.Zero(usage.UserID) && valueor.Zero(existing.ProviderName) == valueor.Zero(usage.ProviderName) && valueor.Zero(existing.ModelName) == valueor.Zero(usage.ModelName) && valueor.Zero(existing.IntervalType) == intervalType && valueor.Zero(existing.StartedAt).Unix() == startedAt.Unix() {
			*existing.PromptTokens += valueor.Zero(usage.PromptTokens)
			*existing.CompletionTokens += valueor.Zero(usage.CompletionTokens)
			*existing.CacheCreationTokens += valueor.Zero(usage.CacheCreationTokens)
			*existing.CacheReadTokens += valueor.Zero(usage.CacheReadTokens)
			*existing.TotalTokens += valueor.Zero(usage.TotalTokens)
			*existing.RequestCount += valueor.Zero(usage.RequestCount)
			return usages
		}
	}
	return append(usages, &models.Usage{
		UserID:              usage.UserID,
		ProviderName:        usage.ProviderName,
		ModelName:           usage.ModelName,
		IntervalType:        ptrto.Value(intervalType),
		StartedAt:           ptrto.Value(startedAt),
		PromptTokens:        ptrto.Value(valueor.Zero(usage.PromptTokens)),
		CompletionTokens:    ptrto.Value(valueor.Zero(usage.CompletionTokens)),
		CacheCreationTokens: ptrto.Value(valueor.Zero(usage.CacheCreationTokens)),
		CacheReadTokens:     ptrto.Value(valueor.Zero(usage.CacheReadTokens)),
		TotalTokens:         ptrto.Value(valueor.Zero(usage.TotalTokens)),
		RequestCount:        ptrto.Value(valueor.Zero(usage.RequestCount)),
	})
}

func (self *fileSystemTransaction) ListUsages(ctx context.Context, listOptions store.UsageListOptions, options *store.Option) ([]*models.Usage, error) {
	usages, err := self.readUsages()
	if err != nil {
		return nil, err
	}
	var result []*models.Usage
	for _, entry := range usages {
		if listOptions.UserID != nil && valueor.Zero(entry.UserID) != *listOptions.UserID {
			continue
		}
		if valueor.Zero(entry.IntervalType) != listOptions.IntervalType {
			continue
		}
		if valueor.Zero(entry.StartedAt).Before(listOptions.StartedAt) {
			continue
		}
		if !valueor.Zero(entry.StartedAt).Before(listOptions.EndedAt) {
			continue
		}
		if listOptions.ProviderName != nil && valueor.Zero(entry.ProviderName) != *listOptions.ProviderName {
			continue
		}
		if listOptions.ModelName != nil && valueor.Zero(entry.ModelName) != *listOptions.ModelName {
			continue
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return valueor.Zero(result[i].StartedAt).Before(valueor.Zero(result[j].StartedAt))
	})
	return result, nil
}
