package fs

import (
	"errors"
	"fmt"
	"os"
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

type transaction struct {
	store *fileSystemStore
}

func Open(options Options) (store.Store, error) {
	return &fileSystemStore{dataDirectory: options.DataDirectory}, nil
}

func (self *fileSystemStore) Close() error {
	return nil
}

func (self *fileSystemStore) Migrate() error {
	return nil
}

func (self *fileSystemStore) Transaction(run func(store.Transaction) error) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	storeTransaction := &transaction{store: self}
	return run(storeTransaction)
}

func (self *transaction) Commit() error { return nil }

func (self *transaction) workspaceRoot(scope models.Scope, scopeId string) (string, error) {
	rootDirectory := self.workspaceDirectory(scope, scopeId)
	if _, err := os.Stat(rootDirectory); errors.Is(err, os.ErrNotExist) {
		return rootDirectory, nil
	}
	return rootDirectory, nil
}

func (self *transaction) workspaceDirectory(scope models.Scope, scopeId string) string {
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

func (self *transaction) workspaceFilePath(scope models.Scope, scopeId string, relativePath string) (string, error) {
	workspaceDirectory := self.workspaceDirectory(scope, scopeId)
	if workspaceDirectory == "" {
		return "", fmt.Errorf("unknown scope: %s", scope)
	}
	normalizedPath := normalizeRelativePath(relativePath)
	if normalizedPath == "." || normalizedPath == "" {
		return "", fmt.Errorf("relative path is required")
	}
	if strings.HasPrefix(normalizedPath, "../") || strings.Contains(normalizedPath, "/../") {
		return "", fmt.Errorf("invalid path")
	}
	absolutePath := filepath.Join(workspaceDirectory, normalizedPath)
	if !strings.HasPrefix(filepath.Clean(absolutePath), filepath.Clean(workspaceDirectory)+string(filepath.Separator)) {
		return "", fmt.Errorf("path escape is not allowed")
	}
	return absolutePath, nil
}

func normalizeRelativePath(relativePath string) string {
	normalizedPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relativePath)))
	return strings.TrimPrefix(normalizedPath, "/")
}

func valueOrEmptyStopReason(value *models.StopReason) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}

func uint64Value(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func sliceValue(value *[]string) []string {
	if value == nil {
		return nil
	}
	valuesCopy := make([]string, 0, len(*value))
	for _, entry := range *value {
		trimmedValue := strings.TrimSpace(entry)
		if trimmedValue == "" {
			continue
		}
		valuesCopy = append(valuesCopy, trimmedValue)
	}
	return valuesCopy
}
