package db

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/teanode/teanode/internal/store"
)

func applyOption(database *gorm.DB, options *store.Option) *gorm.DB {
	if options == nil {
		return database
	}
	if options.Offset != nil {
		database = database.Offset(int(*options.Offset))
	}
	if options.Limit != nil {
		database = database.Limit(int(*options.Limit))
	}
	return database
}

func databaseError(errorValue error) error {
	if errorValue == nil {
		return nil
	}
	if errors.Is(errorValue, gorm.ErrRecordNotFound) {
		return store.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(errorValue, &postgresError) {
		if postgresError.Code == "23505" {
			return store.ErrAlreadyExists
		}
	}
	return errorValue
}

func valueOrTime(value *time.Time) time.Time {
	if value == nil {
		return time.Now().UTC()
	}
	return value.UTC()
}

func normalizeWorkspacePath(path string) string {
	normalizedPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	return strings.TrimPrefix(normalizedPath, "/")
}

func isInvalidWorkspacePath(path string) bool {
	if path == "." || path == "" {
		return true
	}
	if strings.HasPrefix(path, "../") {
		return true
	}
	return strings.Contains(path, "/../")
}
