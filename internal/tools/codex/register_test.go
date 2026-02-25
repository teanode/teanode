package codex

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
	cleanup := makeFakeBinary(t, "codex")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, nil)

	if registry.Get("codex") == nil {
		t.Error("expected codex to be registered with nil config")
	}
}

func TestRegisterTools_NilConfig_BinaryMissing(t *testing.T) {
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	defer os.Setenv("PATH", origPath)

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, nil)

	if registry.Get("codex") != nil {
		t.Error("expected no codex tool when binary is missing")
	}
}

func TestRegisterTools_ExplicitConfig_UsesDefaults(t *testing.T) {
	cleanup := makeFakeBinary(t, "codex")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, &RegistrationOptions{})

	if registry.Get("codex") == nil {
		t.Error("expected codex to be registered with empty config")
	}
}

func TestRegisterTools_ExplicitConfig_CustomBinaryPath(t *testing.T) {
	dir := t.TempDir()
	customBinary := filepath.Join(dir, "my-codex")
	if err := os.WriteFile(customBinary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("creating fake binary: %v", err)
	}

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, &RegistrationOptions{
		BinaryPath: customBinary,
	})

	if registry.Get("codex") == nil {
		t.Error("expected codex to be registered with custom binary path")
	}
}
