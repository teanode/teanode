package configs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSecurity_NormalizesMissingUsernames(t *testing.T) {
	root := t.TempDir()
	SetDirectory(root)
	t.Cleanup(func() { SetDirectory("") })

	securityFile := filepath.Join(root, "security.yaml")
	if err := os.WriteFile(securityFile, []byte(`
users:
  user-a:
    passwordHash: hash-a
  user-b:
    username: user
    passwordHash: hash-b
`), 0600); err != nil {
		t.Fatalf("write security.yaml: %v", err)
	}

	securityConfig, err := LoadSecurity()
	if err != nil {
		t.Fatalf("LoadSecurity: %v", err)
	}

	if securityConfig.Users["user-a"].Username != "user-2" {
		t.Fatalf("normalized username = %q, want %q", securityConfig.Users["user-a"].Username, "user-2")
	}
}

func TestSaveSecurity_NormalizesMissingUsernames(t *testing.T) {
	root := t.TempDir()
	SetDirectory(root)
	t.Cleanup(func() { SetDirectory("") })

	securityConfig := &SecurityConfig{
		Users: map[string]SecurityUser{
			"user-1": {Username: "alice", PasswordHash: "a"},
			"user-2": {PasswordHash: "b"},
		},
	}

	if err := SaveSecurity(securityConfig); err != nil {
		t.Fatalf("SaveSecurity: %v", err)
	}

	loaded, err := LoadSecurity()
	if err != nil {
		t.Fatalf("LoadSecurity: %v", err)
	}

	if loaded.Users["user-2"].Username != "user" {
		t.Fatalf("normalized username = %q, want %q", loaded.Users["user-2"].Username, "user")
	}
}
