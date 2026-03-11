package claudecode

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
	t.Setenv("PATH", directory+string(os.PathListSeparator)+originalPath)
	return func() {}
}

func TestCreateTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "claude")
	defer cleanup()

	tools := createTools()
	found := false
	for _, tool := range tools {
		if tool.Definition().Function.Name == "claude_code" {
			found = true
		}
	}
	if !found {
		t.Error("expected claude_code to be created")
	}
}

func TestCreateTools_BinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tools := createTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools when binary is missing, got %d", len(tools))
	}
}
