package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUserProfile_MissingFallsBackToOSUsername(t *testing.T) {
	withTempDir(t)

	profile, err := LoadUserProfile("user-1")
	if err != nil {
		t.Fatalf("LoadUserProfile failed: %v", err)
	}
	if profile.Name != OSUsername() {
		t.Fatalf("name = %q, want %q", profile.Name, OSUsername())
	}
	if profile.AvatarMediaID != "" {
		t.Fatalf("avatarMediaId = %q, want empty", profile.AvatarMediaID)
	}
}

func TestSaveAndLoadUserProfile_UsesUserYAMLPath(t *testing.T) {
	directory := withTempDir(t)

	input := &UserProfile{
		Name:          "Alice",
		Description:   "Loves concise answers",
		AvatarMediaID: "media_123",
	}
	if err := SaveUserProfile("user-1", input); err != nil {
		t.Fatalf("SaveUserProfile failed: %v", err)
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

	loaded, err := LoadUserProfile("user-1")
	if err != nil {
		t.Fatalf("LoadUserProfile failed: %v", err)
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

func TestLoadUserProfile_IgnoresLegacyMarkdownProfile(t *testing.T) {
	directory := withTempDir(t)
	legacy := filepath.Join(directory, "users", "user-1.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := strings.Join([]string{
		"---",
		"name: Legacy Name",
		"avatarMediaId: legacy_avatar",
		"---",
		"",
		"# Old Bio",
	}, "\n")
	if err := os.WriteFile(legacy, []byte(content), 0644); err != nil {
		t.Fatalf("write legacy profile: %v", err)
	}

	profile, err := LoadUserProfile("user-1")
	if err != nil {
		t.Fatalf("LoadUserProfile failed: %v", err)
	}
	if profile.Name != OSUsername() {
		t.Fatalf("name = %q, want %q", profile.Name, OSUsername())
	}
}

func TestSaveUserProfile_Writes0600Permissions(t *testing.T) {
	directory := withTempDir(t)
	if err := SaveUserProfile("user-1", &UserProfile{Name: "Alice"}); err != nil {
		t.Fatalf("SaveUserProfile failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(directory, "users", "user-1", "user.yaml"))
	if err != nil {
		t.Fatalf("stat profile file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("profile permissions = %o, want %o", info.Mode().Perm(), 0600)
	}
}
