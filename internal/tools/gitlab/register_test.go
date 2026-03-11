package gitlab

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
	cleanup := makeFakeBinary(t, "glab")
	defer cleanup()

	tools := createTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Definition().Function.Name] = true
	}

	// Default services: issues, merge_requests, projects, todos → 4 tools.
	if !names["gitlab_issues"] {
		t.Error("expected gitlab_issues to be created")
	}
	if !names["gitlab_merge_requests"] {
		t.Error("expected gitlab_merge_requests to be created")
	}
	if !names["gitlab_projects"] {
		t.Error("expected gitlab_projects to be created")
	}
	if !names["gitlab_todos"] {
		t.Error("expected gitlab_todos to be created")
	}
}

func TestCreateTools_BinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tools := createTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools when binary is missing, got %d", len(tools))
	}
}
