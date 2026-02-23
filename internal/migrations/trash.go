package migrations

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/trash"
)

// MigrateLegacyTrashDirectories consolidates known legacy per-root .trash
// directories into the single global ~/.teanode/.trash directory.
func MigrateLegacyTrashDirectories() error {
	rootDirectory, err := configs.Directory()
	if err != nil {
		return err
	}
	globalTrashDirectory, err := configs.TrashDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(globalTrashDirectory, 0755); err != nil {
		return fmt.Errorf("creating global trash directory: %w", err)
	}

	for _, legacyTrashDirectory := range legacyTrashDirectories(rootDirectory, globalTrashDirectory) {
		if err := migrateLegacyTrashDirectory(legacyTrashDirectory, globalTrashDirectory); err != nil {
			return err
		}
	}
	return nil
}

func legacyTrashDirectories(rootDirectory string, globalTrashDirectory string) []string {
	candidates := []string{
		filepath.Join(rootDirectory, "media", ".trash"),
		filepath.Join(rootDirectory, "sessions", ".trash"),
		filepath.Join(rootDirectory, "workspace", ".trash"),
		filepath.Join(rootDirectory, "conversations", ".trash"),
	}
	globs := []string{
		filepath.Join(rootDirectory, "users", "*", ".trash"),
		filepath.Join(rootDirectory, "projects", "*", ".trash"),
		filepath.Join(rootDirectory, "agents", "*", ".trash"),
	}
	for _, pattern := range globs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		candidates = append(candidates, matches...)
	}

	seen := map[string]bool{}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		cleanedCandidate := filepath.Clean(candidate)
		if cleanedCandidate == filepath.Clean(globalTrashDirectory) {
			continue
		}
		if seen[cleanedCandidate] {
			continue
		}
		seen[cleanedCandidate] = true
		result = append(result, cleanedCandidate)
	}
	return result
}

func migrateLegacyTrashDirectory(sourceDirectory string, globalTrashDirectory string) error {
	info, err := os.Stat(sourceDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stating legacy trash directory %s: %w", sourceDirectory, err)
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(sourceDirectory)
	if err != nil {
		return fmt.Errorf("reading legacy trash directory %s: %w", sourceDirectory, err)
	}
	for _, entry := range entries {
		path := filepath.Join(sourceDirectory, entry.Name())
		if err := trash.Move(path, globalTrashDirectory); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("moving legacy trash entry %s: %w", path, err)
		}
	}

	entries, err = os.ReadDir(sourceDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading legacy trash directory %s after migration: %w", sourceDirectory, err)
	}
	if len(entries) == 0 {
		if err := os.Remove(sourceDirectory); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing legacy trash directory %s: %w", sourceDirectory, err)
		}
	}
	return nil
}
