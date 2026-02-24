package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUserConfig_MissingFallsBackToOSUsername(t *testing.T) {
	withTempDir(t)

	profile, err := LoadUserConfig("user-1")
	if err != nil {
		t.Fatalf("LoadUserConfig failed: %v", err)
	}
	if profile.Name != OSUsername() {
		t.Fatalf("name = %q, want %q", profile.Name, OSUsername())
	}
	if profile.AvatarMediaID != "" {
		t.Fatalf("avatarMediaId = %q, want empty", profile.AvatarMediaID)
	}
}

func TestSaveAndLoadUserConfig_UsesUserYAMLPath(t *testing.T) {
	directory := withTempDir(t)

	input := &UserConfig{
		Name:          "Alice",
		Description:   "Loves concise answers",
		AvatarMediaID: "media_123",
	}
	if err := SaveUserConfig("user-1", input); err != nil {
		t.Fatalf("SaveUserConfig failed: %v", err)
	}

	path := filepath.Join(directory, "users", "user-1", "user.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected profile file at %s: %v", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile file: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "bio:") {
		t.Fatalf("profile file should not include bio: %q", text)
	}
	if !strings.Contains(text, "name: Alice") {
		t.Fatalf("profile file missing name field: %q", text)
	}
	if !strings.Contains(text, "avatarMediaId: media_123") {
		t.Fatalf("profile file missing avatarMediaId field: %q", text)
	}
	if !strings.Contains(text, "description: Loves concise answers") {
		t.Fatalf("profile file missing description field: %q", text)
	}

	loaded, err := LoadUserConfig("user-1")
	if err != nil {
		t.Fatalf("LoadUserConfig failed: %v", err)
	}
	if loaded.Name != "Alice" {
		t.Fatalf("name = %q, want Alice", loaded.Name)
	}
	if loaded.AvatarMediaID != "media_123" {
		t.Fatalf("avatarMediaId = %q, want media_123", loaded.AvatarMediaID)
	}
	if loaded.Description != "Loves concise answers" {
		t.Fatalf("description = %q, want %q", loaded.Description, "Loves concise answers")
	}
}

func TestSaveUserConfig_Writes0600Permissions(t *testing.T) {
	directory := withTempDir(t)
	if err := SaveUserConfig("user-1", &UserConfig{Name: "Alice"}); err != nil {
		t.Fatalf("SaveUserConfig failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(directory, "users", "user-1", "user.yaml"))
	if err != nil {
		t.Fatalf("stat profile file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("profile permissions = %o, want %o", info.Mode().Perm(), 0600)
	}
}
