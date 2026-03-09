package github

import (
	"os"
	"path/filepath"
	"testing"
)

// makeFakeBinary creates a minimal executable in a temp directory and
// prepends it to PATH so exec.LookPath finds it. Returns a cleanup function.
func makeFakeBinary(t *testing.T, name string) func() {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating fake binary: %v", err)
	}
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", directory+string(os.PathListSeparator)+origPath)
	return func() { os.Setenv("PATH", origPath) }
}

func TestCreateTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "gh")
	defer cleanup()

	tools := createTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Definition().Function.Name] = true
	}

	// Default services: issues, pulls, repos → 3 tools.
	if !names["github_issues"] {
		t.Error("expected github_issues to be created")
	}
	if !names["github_pulls"] {
		t.Error("expected github_pulls to be created")
	}
	if !names["github_repos"] {
		t.Error("expected github_repos to be created")
	}
}

func TestCreateTools_BinaryMissing(t *testing.T) {
	// Use a PATH with no gh binary.
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	tools := createTools()
	if len(tools) != 0 {
		t.Errorf("expected no tools when binary is missing, got %d", len(tools))
	}
}
