package gitea

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
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() {}
}

func TestCreateTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "tea")
	defer cleanup()

	tools := createTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Definition().Function.Name] = true
	}

	// Default services: issues, pulls, repos → 3 tools.
	if !names["gitea_issues"] {
		t.Error("expected gitea_issues to be created")
	}
	if !names["gitea_pulls"] {
		t.Error("expected gitea_pulls to be created")
	}
	if !names["gitea_repos"] {
		t.Error("expected gitea_repos to be created")
	}
}

func TestCreateTools_BinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tools := createTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools when binary is missing, got %d", len(tools))
	}
}
