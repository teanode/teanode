package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveFile(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "data", "note.txt")
	trashDirectory := filepath.Join(base, ".trash")

	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(source, []byte("hello"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := Move(source, trashDirectory); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, err=%v", err)
	}

	entries, err := os.ReadDir(trashDirectory)
	if err != nil {
		t.Fatalf("ReadDir trash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 trashed file, got %d", len(entries))
	}
	if !strings.Contains(entries[0].Name(), "note.txt") {
		t.Fatalf("expected trash name to include original filename, got %q", entries[0].Name())
	}
	if strings.Contains(entries[0].Name(), "tmp") {
		t.Fatalf("trash name should be relative to data root, got %q", entries[0].Name())
	}

	trashedPath := filepath.Join(trashDirectory, entries[0].Name())
	data, err := os.ReadFile(trashedPath)
	if err != nil {
		t.Fatalf("ReadFile trashed: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("trashed content = %q, want hello", string(data))
	}
}

func TestMoveDirectory(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "workspace", "agent-a")
	trashDirectory := filepath.Join(base, ".trash")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "AGENT.md"), []byte("be concise"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := Move(source, trashDirectory); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("expected source directory to be removed, err=%v", err)
	}

	entries, err := os.ReadDir(trashDirectory)
	if err != nil {
		t.Fatalf("ReadDir trash: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 trashed directory, got %d", len(entries))
	}
	if !entries[0].IsDir() {
		t.Fatalf("expected trashed entry to be a directory")
	}

	trashedFile := filepath.Join(trashDirectory, entries[0].Name(), "AGENT.md")
	data, err := os.ReadFile(trashedFile)
	if err != nil {
		t.Fatalf("ReadFile trashed directory content: %v", err)
	}
	if string(data) != "be concise" {
		t.Fatalf("trashed AGENT.md content = %q, want be concise", string(data))
	}
}
