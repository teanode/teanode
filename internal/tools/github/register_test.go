package github

import (
	"os"
	"path/filepath"
	"testing"

	toolregistry "github.com/teanode/teanode/internal/tools"
)

// makeFakeBinary creates a minimal executable in a temp directory and
// prepends it to PATH so exec.LookPath finds it. Returns a cleanup function.
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

func TestRegisterTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "gh")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry)

	// Default services: issues, pulls, repos → 3 tools.
	if registry.Get("github_issues") == nil {
		t.Error("expected github_issues to be registered")
	}
	if registry.Get("github_pulls") == nil {
		t.Error("expected github_pulls to be registered")
	}
	if registry.Get("github_repos") == nil {
		t.Error("expected github_repos to be registered")
	}
}

func TestRegisterTools_BinaryMissing(t *testing.T) {
	// Use a PATH with no gh binary.
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry)

	if len(registry.Names()) != 0 {
		t.Errorf("expected no tools when binary is missing, got %v", registry.Names())
	}
}
