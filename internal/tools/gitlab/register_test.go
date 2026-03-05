package gitlab

import (
	"os"
	"path/filepath"
	"testing"
)

func makeFakeBinary(t *testing.T, name string) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating fake binary: %v", err)
	}
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+origPath)
	return func() { os.Setenv("PATH", origPath) }
}

func TestCreateTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "glab")
	defer cleanup()

	tools := createTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Definition().Function.Name] = true
	}

	// Default services: issues, merge_requests, projects → 3 tools.
	if !names["gitlab_issues"] {
		t.Error("expected gitlab_issues to be created")
	}
	if !names["gitlab_merge_requests"] {
		t.Error("expected gitlab_merge_requests to be created")
	}
	if !names["gitlab_projects"] {
		t.Error("expected gitlab_projects to be created")
	}
}

func TestCreateTools_BinaryMissing(t *testing.T) {
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	tools := createTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools when binary is missing, got %d", len(tools))
	}
}
