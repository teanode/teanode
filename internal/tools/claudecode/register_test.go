package claudecode

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

func TestRegisterTools_BinaryPresent(t *testing.T) {
	cleanup := makeFakeBinary(t, "claude")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry)

	if registry.Get("claude_code") == nil {
		t.Error("expected claude_code to be registered")
	}
}

func TestRegisterTools_BinaryMissing(t *testing.T) {
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry)

	if registry.Get("claude_code") != nil {
		t.Error("expected no claude_code tool when binary is missing")
	}
}
