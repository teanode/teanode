package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func makeFakeBinary(t *testing.T, name string) func() {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating fake binary: %v", err)
	}
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	return func() { os.Setenv("PATH", originalPath) }
}

func TestCreateTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "codex")
	defer cleanup()

	tools := createTools()
	found := false
	for _, tool := range tools {
		if tool.Definition().Function.Name == "codex" {
			found = true
		}
	}
	if !found {
		t.Error("expected codex to be created")
	}
}

func TestCreateTools_BinaryMissing(t *testing.T) {
	originalPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", originalPath)

	tools := createTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools when binary is missing, got %d", len(tools))
	}
}
