package fsstore

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

func (self *fileSystemTransaction) CreateModelUsageEvent(ctx context.Context, event *models.ModelUsageEvent, options *store.Option) error {
	return store.ErrNotImplemented
}

func (self *fileSystemTransaction) UpsertModelUsageStatEntry(ctx context.Context, event *models.ModelUsageEvent, intervalType models.IntervalType, startedAt time.Time, options *store.Option) error {
	return store.ErrNotImplemented
}

func (self *fileSystemTransaction) QueryModelUsageStatEntries(ctx context.Context, query store.ModelUsageStatQuery, options *store.Option) ([]*models.ModelUsageStatEntry, error) {
	return nil, store.ErrNotImplemented
}
