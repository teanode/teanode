package fsstore

import (
	"context"
	"os"
	"sort"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/vmihailenco/msgpack/v5"
)

// Default retention limits per (userId, providerName, modelName, intervalType).
const (
	defaultMaxHourlyEntries = 168 // 7 days
	defaultMaxDailyEntries  = 90  // 90 days
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
		IntervalType models.IntervalType
	}
	groups := map[groupKey][]*models.Usage{}
	for _, u := range usages {
		k := groupKey{u.UserID, u.ProviderName, u.ModelName, u.IntervalType}
		groups[k] = append(groups[k], u)
	}
	var result []*models.Usage
	for k, entries := range groups {
		maxEntries := defaultMaxDailyEntries
		if k.IntervalType == models.IntervalHourly {
			maxEntries = defaultMaxHourlyEntries
		}
		if len(entries) > maxEntries {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].StartedAt.Before(entries[j].StartedAt)
			})
			entries = entries[len(entries)-maxEntries:]
		}
		result = append(result, entries...)
	}
	return result
}

func (self *fileSystemTransaction) UpsertUsage(ctx context.Context, usage *models.Usage, options *store.Option) error {
	if usage == nil {
		return store.ErrInvalidOptions
	}
	usages, err := self.readUsages()
	if err != nil {
		return err
	}
	key := usage.Key()
	found := false
	for _, u := range usages {
		if u.Key() == key {
			u.PromptTokens += usage.PromptTokens
			u.CompletionTokens += usage.CompletionTokens
			u.CacheCreationTokens += usage.CacheCreationTokens
			u.CacheReadTokens += usage.CacheReadTokens
			u.TotalTokens += usage.TotalTokens
			u.RequestCount += usage.RequestCount
			found = true
			break
		}
	}
	if !found {
		usages = append(usages, &models.Usage{
			UserID:              usage.UserID,
			ProviderName:        usage.ProviderName,
			ModelName:           usage.ModelName,
			IntervalType:        usage.IntervalType,
			StartedAt:           usage.StartedAt,
			PromptTokens:        usage.PromptTokens,
			CompletionTokens:    usage.CompletionTokens,
			CacheCreationTokens: usage.CacheCreationTokens,
			CacheReadTokens:     usage.CacheReadTokens,
			TotalTokens:         usage.TotalTokens,
			RequestCount:        usage.RequestCount,
		})
	}
	usages = evictUsages(usages)
	return self.writeUsages(usages)
}

func (self *fileSystemTransaction) QueryUsages(ctx context.Context, query store.UsageQuery, options *store.Option) ([]*models.Usage, error) {
	usages, err := self.readUsages()
	if err != nil {
		return nil, err
	}
	var result []*models.Usage
	for _, u := range usages {
		if u.UserID != query.UserID {
			continue
		}
		if u.IntervalType != query.IntervalType {
			continue
		}
		if u.StartedAt.Before(query.StartedAt) {
			continue
		}
		if !u.StartedAt.Before(query.EndedAt) {
			continue
		}
		if query.ProviderName != nil && u.ProviderName != *query.ProviderName {
			continue
		}
		if query.ModelName != nil && u.ModelName != *query.ModelName {
			continue
		}
		result = append(result, u)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StartedAt.Before(result[j].StartedAt)
	})
	return result, nil
}
