package dbmigrations

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.sql
var migrationFiles embed.FS

type Migration struct {
	ID         string
	SQL        string
	ReverseSQL string
}

var migrationCache = mustLoadMigrations()

func Migrations() []Migration {
	return migrationCache
}

func mustLoadMigrations() []Migration {
	entries, err := fs.ReadDir(migrationFiles, ".")
	if err != nil {
		panic(fmt.Errorf("failed to read migrations: %w", err))
	}

	forward := make(map[string]string)
	reverse := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		content, err := migrationFiles.ReadFile(name)
		if err != nil {
			panic(fmt.Errorf("failed to read migration file %q: %w", name, err))
		}
		text := string(content)
		if strings.HasSuffix(name, ".reverse.sql") {
			migrationId := strings.TrimSuffix(name, ".reverse.sql")
			reverse[migrationId] = text
			continue
		}
		migrationId := strings.TrimSuffix(name, ".sql")
		forward[migrationId] = text
	}

	for migrationId := range forward {
		if _, ok := reverse[migrationId]; !ok {
			panic(fmt.Sprintf("missing reverse migration for %s", migrationId))
		}
	}
	for migrationId := range reverse {
		if _, ok := forward[migrationId]; !ok {
			panic(fmt.Sprintf("missing forward migration for %s", migrationId))
		}
	}

	migrationIds := make([]string, 0, len(forward))
	for migrationId := range forward {
		migrationIds = append(migrationIds, migrationId)
	}
	sort.Strings(migrationIds)

	result := make([]Migration, 0, len(migrationIds))
	for _, migrationId := range migrationIds {
		result = append(result, Migration{
			ID:         migrationId,
			SQL:        forward[migrationId],
			ReverseSQL: reverse[migrationId],
		})
	}
	return result
}
