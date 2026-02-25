package skills

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fs"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/timeutil"
)

func setupSkillStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

func TestListInstalledWithMinimalMetadata(t *testing.T) {
	openedStore := setupSkillStore(t)
	version := "1.0.0"
	createError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, skillCreateError := transaction.CreateSkill(&models.Skill{
			ID:      "demo",
			Name:    ptrto.Value("demo"),
			Version: &version,
		}, nil)
		return skillCreateError
	})
	if createError != nil {
		t.Fatalf("creating skill: %v", createError)
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	installed, listError := ListInstalled(ctx)
	if listError != nil {
		t.Fatalf("ListInstalled: %v", listError)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}
	if installed[0].Name != "demo" {
		t.Fatalf("name = %q, want demo", installed[0].Name)
	}
	if installed[0].Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", installed[0].Version)
	}
	if !installed[0].Enabled {
		t.Fatal("enabled = false, want true by default")
	}
}

func TestListInstalledReadsMetadata(t *testing.T) {
	openedStore := setupSkillStore(t)
	version := "1.0.0"
	installedAt := timeutil.Timestamp{Time: time.UnixMilli(12345)}
	metadata := map[string]interface{}{
		"description": "Demo skill",
		"enabled":     false,
		"sourceId":    "registry",
		"publisher":   "Example",
		"installedAt": installedAt.String(),
	}
	createError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, skillCreateError := transaction.CreateSkill(&models.Skill{
			ID:       "demo",
			Name:     ptrto.Value("demo"),
			Version:  &version,
			Source:   ptrto.Value("registry"),
			Metadata: &metadata,
		}, nil)
		return skillCreateError
	})
	if createError != nil {
		t.Fatalf("creating skill: %v", createError)
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	installed, listError := ListInstalled(ctx)
	if listError != nil {
		t.Fatalf("ListInstalled: %v", listError)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}
	if installed[0].Description != "Demo skill" {
		t.Fatalf("description = %q, want Demo skill", installed[0].Description)
	}
	if installed[0].SourceID != "registry" {
		t.Fatalf("sourceId = %q, want registry", installed[0].SourceID)
	}
	if installed[0].Publisher != "Example" {
		t.Fatalf("publisher = %q, want Example", installed[0].Publisher)
	}
	if installed[0].Enabled {
		t.Fatal("enabled = true, want false")
	}
	if installed[0].InstalledAt.Time.UnixMilli() != 12345 {
		t.Fatalf("installedAt = %d, want 12345", installed[0].InstalledAt.Time.UnixMilli())
	}
}

func TestSetInstalledSkillEnabledPersistsAcrossListInstalled(t *testing.T) {
	openedStore := setupSkillStore(t)
	version := "1.0.0"
	createError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, skillCreateError := transaction.CreateSkill(&models.Skill{
			ID:      "demo",
			Name:    ptrto.Value("demo"),
			Version: &version,
		}, nil)
		return skillCreateError
	})
	if createError != nil {
		t.Fatalf("creating skill: %v", createError)
	}
	ctx := store.ContextWithStore(context.Background(), openedStore)

	if setError := SetInstalledSkillEnabled(ctx, "demo", false); setError != nil {
		t.Fatalf("SetInstalledSkillEnabled(false): %v", setError)
	}
	installed, listError := ListInstalled(ctx)
	if listError != nil {
		t.Fatalf("ListInstalled after disable: %v", listError)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}
	if installed[0].Enabled {
		t.Fatal("enabled = true, want false after disable")
	}

	if setError := SetInstalledSkillEnabled(ctx, "demo", true); setError != nil {
		t.Fatalf("SetInstalledSkillEnabled(true): %v", setError)
	}
	installed, listError = ListInstalled(ctx)
	if listError != nil {
		t.Fatalf("ListInstalled after enable: %v", listError)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}
	if !installed[0].Enabled {
		t.Fatal("enabled = false, want true after enable")
	}
}
