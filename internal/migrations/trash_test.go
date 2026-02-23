package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/configs"
)

func TestMigrateLegacyTrashDirectories_MovesKnownRoots(t *testing.T) {
	root := t.TempDir()
	configs.SetDirectory(root)
	t.Cleanup(func() { configs.SetDirectory("") })

	if err := configs.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	legacyFiles := []string{
		filepath.Join(root, "users", "user-1", ".trash", "user-note.txt"),
		filepath.Join(root, "projects", "project-1", ".trash", "project-note.txt"),
		filepath.Join(root, "agents", "agent-1", ".trash", "agent-note.txt"),
	}
	for _, path := range legacyFiles {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("legacy"), 0644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	unknownTrash := filepath.Join(root, "users", "user-1", "workspace", ".trash")
	if err := os.MkdirAll(unknownTrash, 0755); err != nil {
		t.Fatalf("MkdirAll unknownTrash: %v", err)
	}
	unknownFile := filepath.Join(unknownTrash, "keep.txt")
	if err := os.WriteFile(unknownFile, []byte("keep"), 0644); err != nil {
		t.Fatalf("WriteFile unknownFile: %v", err)
	}

	if err := MigrateLegacyTrashDirectories(); err != nil {
		t.Fatalf("MigrateLegacyTrashDirectories: %v", err)
	}

	for _, path := range legacyFiles {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy file should be moved: %s", path)
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Fatalf("legacy trash directory should be removed when empty: %s", filepath.Dir(path))
		}
	}

	if _, err := os.Stat(unknownFile); err != nil {
		t.Fatalf("unknown nested .trash should not be touched: %v", err)
	}

	globalTrashDirectory, err := configs.TrashDirectory()
	if err != nil {
		t.Fatalf("TrashDirectory: %v", err)
	}
	entries, err := os.ReadDir(globalTrashDirectory)
	if err != nil {
		t.Fatalf("ReadDir global trash: %v", err)
	}
	if len(entries) < len(legacyFiles) {
		t.Fatalf("global trash entries = %d, want at least %d", len(entries), len(legacyFiles))
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, expected := range []string{"user-note.txt", "project-note.txt", "agent-note.txt"} {
		found := false
		for _, name := range names {
			if strings.Contains(name, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected trashed entry containing %q, names=%v", expected, names)
		}
	}
}

func TestMigrateLegacyTrashDirectories_Idempotent(t *testing.T) {
	root := t.TempDir()
	configs.SetDirectory(root)
	t.Cleanup(func() { configs.SetDirectory("") })

	if err := configs.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	legacyFile := filepath.Join(root, "users", "user-1", ".trash", "legacy.txt")
	if err := os.MkdirAll(filepath.Dir(legacyFile), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(legacyFile, []byte("legacy"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := MigrateLegacyTrashDirectories(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	globalTrashDirectory, err := configs.TrashDirectory()
	if err != nil {
		t.Fatalf("TrashDirectory: %v", err)
	}
	firstEntries, err := os.ReadDir(globalTrashDirectory)
	if err != nil {
		t.Fatalf("ReadDir after first migrate: %v", err)
	}

	if err := MigrateLegacyTrashDirectories(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	secondEntries, err := os.ReadDir(globalTrashDirectory)
	if err != nil {
		t.Fatalf("ReadDir after second migrate: %v", err)
	}
	if len(secondEntries) != len(firstEntries) {
		t.Fatalf("global trash entry count changed across idempotent run: first=%d second=%d", len(firstEntries), len(secondEntries))
	}
}
