package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/timeutil"
)

func TestIsSafePathSegment(t *testing.T) {
	tests := []struct {
		value string
		safe  bool
	}{
		{value: "git", safe: true},
		{value: "1.0.0", safe: true},
		{value: "", safe: false},
		{value: ".", safe: false},
		{value: "..", safe: false},
		{value: "../x", safe: false},
		{value: "a/b", safe: false},
		{value: `a\b`, safe: false},
	}
	for _, testCase := range tests {
		if got := isSafePathSegment(testCase.value); got != testCase.safe {
			t.Fatalf("isSafePathSegment(%q) = %v, want %v", testCase.value, got, testCase.safe)
		}
	}
}

func TestResolveInstallDirRejectsTraversal(t *testing.T) {
	if _, err := resolveInstallDir("/tmp/skills", "../escape", "1.0.0"); err == nil {
		t.Fatal("expected invalid skill name error")
	}
	if _, err := resolveInstallDir("/tmp/skills", "git", "../../escape"); err == nil {
		t.Fatal("expected invalid skill version error")
	}
}

func TestEnsureNoSymlinkComponentsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "git")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := ensureNoSymlinkComponents(root, filepath.Join(link, "1.0.0")); err == nil {
		t.Fatal("expected symlink component rejection")
	}
}

func TestListInstalledWithMinimalManifest(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	installDirectory := filepath.Join(directory, "skills", ".installed", "demo", "1.0.0")
	if err := os.MkdirAll(installDirectory, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := installManifest{
		Name:    "demo",
		Version: "1.0.0",
	}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(installDirectory, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("manifest write: %v", err)
	}

	installed, err := ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
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

func TestListInstalledReadsManifestMetadata(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	installDirectory := filepath.Join(directory, "skills", ".installed", "demo", "1.0.0")
	if err := os.MkdirAll(installDirectory, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := installManifest{
		Name:        "demo",
		Description: "Demo skill",
		Version:     "1.0.0",
		SourceID:    "registry",
		Publisher:   "Example",
		InstalledAt: timeutil.Timestamp{Time: time.UnixMilli(12345)},
	}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(installDirectory, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("manifest write: %v", err)
	}

	installed, err := ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
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
	if installed[0].InstalledAt.Time.UnixMilli() != 12345 {
		t.Fatalf("installedAt = %d, want 12345", installed[0].InstalledAt.Time.UnixMilli())
	}
}

func TestSetInstalledSkillEnabledPersistsAcrossListInstalled(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	installDirectory := filepath.Join(directory, "skills", ".installed", "demo", "1.0.0")
	if err := os.MkdirAll(installDirectory, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := installManifest{
		Name:    "demo",
		Version: "1.0.0",
	}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(installDirectory, "manifest.json"), manifestBytes, 0644); err != nil {
		t.Fatalf("manifest write: %v", err)
	}

	if err := SetInstalledSkillEnabled("demo", false); err != nil {
		t.Fatalf("SetInstalledSkillEnabled(false): %v", err)
	}
	installed, err := ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after disable: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}
	if installed[0].Enabled {
		t.Fatal("enabled = true, want false after disable")
	}

	if err := SetInstalledSkillEnabled("demo", true); err != nil {
		t.Fatalf("SetInstalledSkillEnabled(true): %v", err)
	}
	installed, err = ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after enable: %v", err)
	}
	if len(installed) != 1 {
		t.Fatalf("installed count = %d, want 1", len(installed))
	}
	if !installed[0].Enabled {
		t.Fatal("enabled = false, want true after enable")
	}
}
