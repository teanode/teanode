package google

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
	cleanup := makeFakeBinary(t, "gog")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, nil)

	// Default services: gmail, calendar, drive → 3 tools.
	if registry.Get("google_gmail") == nil {
		t.Error("expected google_gmail to be registered with nil config")
	}
	if registry.Get("google_calendar") == nil {
		t.Error("expected google_calendar to be registered with nil config")
	}
	if registry.Get("google_drive") == nil {
		t.Error("expected google_drive to be registered with nil config")
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
	cleanup := makeFakeBinary(t, "gog")
	defer cleanup()

	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry, &RegistrationOptions{
		Services: []string{"gmail", "contacts"},
	})

	if registry.Get("google_gmail") == nil {
		t.Error("expected google_gmail to be registered")
	}
	if registry.Get("google_contacts") == nil {
		t.Error("expected google_contacts to be registered")
	}
	if registry.Get("google_calendar") != nil {
		t.Error("expected google_calendar to NOT be registered (not in custom services)")
	}
}
