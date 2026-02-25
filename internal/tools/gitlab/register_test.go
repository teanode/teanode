package gitlab

import (
	"os"
	"path/filepath"
	"testing"

	toolregistry "github.com/teanode/teanode/internal/tools"
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

func TestRegisterTools_NilConfig_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "glab")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, nil)

	// Default services: issues, merge_requests, projects → 3 tools.
	if registry.Get("gitlab_issues") == nil {
		t.Error("expected gitlab_issues to be registered with nil config")
	}
	if registry.Get("gitlab_merge_requests") == nil {
		t.Error("expected gitlab_merge_requests to be registered with nil config")
	}
	if registry.Get("gitlab_projects") == nil {
		t.Error("expected gitlab_projects to be registered with nil config")
	}
}

func TestRegisterTools_NilConfig_BinaryMissing(t *testing.T) {
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, nil)

	if len(registry.Names()) != 0 {
		t.Errorf("expected no tools when binary is missing, got %v", registry.Names())
	}
}

func TestRegisterTools_ExplicitConfig_CustomServices(t *testing.T) {
	cleanup := makeFakeBinary(t, "glab")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, &RegistrationOptions{
		Services: []string{"issues", "pipelines"},
	})

	if registry.Get("gitlab_issues") == nil {
		t.Error("expected gitlab_issues to be registered")
	}
	if registry.Get("gitlab_pipelines") == nil {
		t.Error("expected gitlab_pipelines to be registered")
	}
	if registry.Get("gitlab_merge_requests") != nil {
		t.Error("expected gitlab_merge_requests to NOT be registered (not in custom services)")
	}
}
