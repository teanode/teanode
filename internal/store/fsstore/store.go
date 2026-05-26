package fsstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

type Options struct {
	DataDirectory string
}

type fileSystemStore struct {
	dataDirectory string
	mutex         sync.Mutex
}

type fileSystemTransaction struct {
	store        *fileSystemStore
	memoryCache  map[string][]*storeMemoryItemRecord
	usagesCache  []*models.Usage
	usagesLoaded bool
}

func Open(options Options) (store.Store, error) {
	return &fileSystemStore{dataDirectory: options.DataDirectory}, nil
}

func (self *fileSystemStore) Close() error {
	return nil
}

func (self *fileSystemStore) Migrate(ctx context.Context) error {
	return nil
}

func (self *fileSystemStore) Transaction(ctx context.Context, run func(context.Context, store.Transaction) error) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	storeTransaction := &fileSystemTransaction{store: self}
	return run(ctx, storeTransaction)
}

func (self *fileSystemTransaction) workspaceRoot(scope models.Scope, scopeId string) (string, error) {
	return self.workspaceDirectory(scope, scopeId), nil
}

func (self *fileSystemTransaction) workspaceDirectory(scope models.Scope, scopeId string) string {
	switch scope {
	case models.ScopeAgent:
		return self.agentWorkspaceDirectory(scopeId)
	case models.ScopeUser:
		return self.userWorkspaceDirectory(scopeId)
	case models.ScopeProject:
		return self.projectWorkspaceDirectory(scopeId)
	default:
		return ""
	}
}

func (self *fileSystemTransaction) workspaceFilePath(scope models.Scope, scopeId string, relativePath string) (string, error) {
	workspaceDirectory := self.workspaceDirectory(scope, scopeId)
	if workspaceDirectory == "" {
		return "", fmt.Errorf("fsstore: unknown scope: %s", scope)
	}
	normalizedPath := normalizeRelativePath(relativePath)
	if normalizedPath == "." || normalizedPath == "" {
		return "", fmt.Errorf("fsstore: relative path is required")
	}
	if strings.HasPrefix(normalizedPath, "../") || strings.Contains(normalizedPath, "/../") {
		return "", fmt.Errorf("fsstore: invalid path")
	}
	absolutePath := filepath.Join(workspaceDirectory, normalizedPath)
	if !strings.HasPrefix(filepath.Clean(absolutePath), filepath.Clean(workspaceDirectory)+string(filepath.Separator)) {
		return "", fmt.Errorf("fsstore: path escape is not allowed")
	}
	return absolutePath, nil
}

func normalizeRelativePath(relativePath string) string {
	normalizedPath := filepath.ToSlash(filepath.Clean(relativePath))
	return strings.TrimPrefix(normalizedPath, "/")
}

func applyOffsetLimit[T any](values []T, options *store.Option) []T {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []T{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}

func uint64Value(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func sliceValue(value *[]string) []string {
	if value == nil {
		return nil
	}
	valuesCopy := make([]string, 0, len(*value))
	for _, entry := range *value {
		trimmedValue := entry
		if trimmedValue == "" {
			continue
		}
		valuesCopy = append(valuesCopy, trimmedValue)
	}
	return valuesCopy
}
