package fsstore

import (
	"context"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

func (self *fileSystemTransaction) CreateMemoryItem(ctx context.Context, item *models.MemoryItem, options *store.Option) (*models.MemoryItem, error) {
	return nil, store.ErrNotImplemented
}

func (self *fileSystemTransaction) GetMemoryItem(ctx context.Context, memoryItemID string, options *store.Option) (*models.MemoryItem, error) {
	return nil, store.ErrNotImplemented
}

func (self *fileSystemTransaction) ModifyMemoryItem(ctx context.Context, memoryItemID string, modifier func(*models.MemoryItem) error, options *store.Option) (*models.MemoryItem, error) {
	return nil, store.ErrNotImplemented
}

func (self *fileSystemTransaction) DeleteMemoryItem(ctx context.Context, memoryItemID string, options *store.Option) error {
	return store.ErrNotImplemented
}

func (self *fileSystemTransaction) ListMemoryItems(ctx context.Context, scope models.Scope, scopeID string, listOptions store.MemoryItemListOptions, options *store.Option) ([]*models.MemoryItem, error) {
	return nil, store.ErrNotImplemented
}

func (self *fileSystemTransaction) SearchMemoryItems(ctx context.Context, scope models.Scope, scopeID string, query string, searchOptions store.MemoryItemSearchOptions, options *store.Option) ([]store.MemoryItemSearchResult, error) {
	return nil, store.ErrNotImplemented
}
