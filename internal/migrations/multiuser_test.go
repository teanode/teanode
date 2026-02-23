package migrations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/teanode/teanode/internal/configs"
	"gopkg.in/yaml.v3"
)

type migratedState struct {
	Users map[string]struct {
		DefaultAgentId         string            `yaml:"defaultAgentId"`
		DefaultConversationIds map[string]string `yaml:"defaultConversationIds"`
	} `yaml:"users"`
}

func TestMigrateMultiUserV2_V1ToV2AndLegacyMoves(t *testing.T) {
	root := t.TempDir()
	configs.SetDirectory(root)
	t.Cleanup(func() { configs.SetDirectory("") })
	if err := configs.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	securityFile := filepath.Join(root, "security.yaml")
	if err := os.WriteFile(securityFile, []byte("token: tok123\npassword: bcrypt-hash\n"), 0600); err != nil {
		t.Fatalf("write security.yaml: %v", err)
	}
	stateFile := filepath.Join(root, "state.yaml")
	if err := os.WriteFile(stateFile, []byte("defaultAgentId: main\ndefaultConversationIds:\n  main: conv-1\n"), 0644); err != nil {
		t.Fatalf("write state.yaml: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "workspace", "main"), 0755); err != nil {
		t.Fatalf("mkdir legacy workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "main", "AGENT.md"), []byte("agent"), 0644); err != nil {
		t.Fatalf("write legacy agent workspace file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "MEMORY.md"), []byte("legacy user memory"), 0644); err != nil {
		t.Fatalf("write legacy user workspace file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "conversations", "main"), 0755); err != nil {
		t.Fatalf("mkdir legacy conversations: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "conversations", "main", "conv-1.jsonl"), []byte(`{"type":"message"}`+"\n"), 0644); err != nil {
		t.Fatalf("write legacy conversation file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "jobs"), 0755); err != nil {
		t.Fatalf("mkdir legacy jobs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "jobs", "job-1.md"), []byte("---\nname: Legacy Job\nenabled: true\n---\n\nhello\n"), 0644); err != nil {
		t.Fatalf("write legacy job file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "profile.md"), []byte("legacy profile"), 0644); err != nil {
		t.Fatalf("write legacy profile: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(root, "projects", "p1"), 0755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "p1", "notes.txt"), []byte("project notes"), 0644); err != nil {
		t.Fatalf("write project file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", "p1.yaml"), []byte("name: p1\n"), 0644); err != nil {
		t.Fatalf("write project metadata: %v", err)
	}

	if err := MigrateMultiUserV2(); err != nil {
		t.Fatalf("MigrateMultiUserV2: %v", err)
	}

	securityConfig, err := configs.LoadSecurity()
	if err != nil {
		t.Fatalf("LoadSecurity: %v", err)
	}
	userId := firstUserId(securityConfig)
	if userId == "" {
		t.Fatal("expected non-empty userId")
	}
	user, ok := securityConfig.Users[userId]
	if !ok {
		t.Fatalf("migrated user %q not found", userId)
	}
	if user.Username != configs.OSUsername() {
		t.Fatalf("username = %q, want %q", user.Username, configs.OSUsername())
	}
	if user.PasswordHash != "bcrypt-hash" {
		t.Fatalf("passwordHash = %q, want %q", user.PasswordHash, "bcrypt-hash")
	}
	if !user.Admin {
		t.Fatalf("admin = %v, want true", user.Admin)
	}
	if len(user.Tokens) != 1 || user.Tokens[0].Token != "tok123" {
		t.Fatalf("tokens = %+v, want one token tok123", user.Tokens)
	}

	stateBytes, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state.yaml: %v", err)
	}
	var migrated migratedState
	if err := yaml.Unmarshal(stateBytes, &migrated); err != nil {
		t.Fatalf("unmarshal state.yaml: %v", err)
	}
	userState, ok := migrated.Users[userId]
	if !ok {
		t.Fatalf("state user %q not found", userId)
	}
	if userState.DefaultAgentId != "main" {
		t.Fatalf("defaultAgentId = %q, want %q", userState.DefaultAgentId, "main")
	}
	if userState.DefaultConversationIds["main"] != "conv-1" {
		t.Fatalf("defaultConversationIds[main] = %q, want %q", userState.DefaultConversationIds["main"], "conv-1")
	}

	agentWorkspace, _ := configs.AgentWorkspaceDirectory("main")
	if _, err := os.Stat(filepath.Join(agentWorkspace, "AGENT.md")); err != nil {
		t.Fatalf("migrated agent workspace file missing: %v", err)
	}
	userWorkspace, _ := configs.UserWorkspaceDirectory(userId)
	if _, err := os.Stat(filepath.Join(userWorkspace, "MEMORY.md")); err != nil {
		t.Fatalf("migrated user workspace file missing: %v", err)
	}
	userConversations, _ := configs.UserAgentConversationsDirectory(userId, "main")
	if _, err := os.Stat(filepath.Join(userConversations, "conv-1.jsonl")); err != nil {
		t.Fatalf("migrated conversation file missing: %v", err)
	}
	userJobs, _ := configs.UserJobsDirectory(userId)
	if _, err := os.Stat(filepath.Join(userJobs, "job-1.md")); err != nil {
		t.Fatalf("migrated job file missing: %v", err)
	}
	userProfile, _ := configs.UserProfileFile(userId)
	if _, err := os.Stat(userProfile); err != nil {
		t.Fatalf("migrated user profile missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "p1", "workspace", "notes.txt")); err != nil {
		t.Fatalf("migrated project workspace file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "p1", "project.yaml")); err != nil {
		t.Fatalf("project metadata should move to project.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "p1.yaml")); !os.IsNotExist(err) {
		t.Fatalf("legacy project metadata should not remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".migrations", multiUserMigrationMarker)); err != nil {
		t.Fatalf("migration marker missing: %v", err)
	}
}

func TestMigrateMultiUserV2_Idempotent(t *testing.T) {
	root := t.TempDir()
	configs.SetDirectory(root)
	t.Cleanup(func() { configs.SetDirectory("") })
	if err := configs.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	securityFile := filepath.Join(root, "security.yaml")
	if err := os.WriteFile(securityFile, []byte("token: tok123\npassword: bcrypt-hash\n"), 0600); err != nil {
		t.Fatalf("write security.yaml: %v", err)
	}

	if err := MigrateMultiUserV2(); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	securityConfig, err := configs.LoadSecurity()
	if err != nil {
		t.Fatalf("LoadSecurity after first migrate: %v", err)
	}
	firstMigratedUserId := firstUserId(securityConfig)
	if firstMigratedUserId == "" {
		t.Fatal("expected first migrated user id")
	}
	if err := MigrateMultiUserV2(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	securityConfig, err = configs.LoadSecurity()
	if err != nil {
		t.Fatalf("LoadSecurity after second migrate: %v", err)
	}
	secondUserId := firstUserId(securityConfig)
	if secondUserId == "" {
		t.Fatal("expected second migrated user id")
	}
	if firstMigratedUserId != secondUserId {
		t.Fatalf("userId changed across migrations: first=%q second=%q", firstMigratedUserId, secondUserId)
	}

	if len(securityConfig.Users) != 1 {
		t.Fatalf("users = %d, want 1", len(securityConfig.Users))
	}
	user := securityConfig.Users[firstMigratedUserId]
	if user.Username != configs.OSUsername() {
		t.Fatalf("username = %q, want %q", user.Username, configs.OSUsername())
	}
	if !user.Admin {
		t.Fatalf("admin = %v, want true", user.Admin)
	}
	if len(user.Tokens) != 1 {
		t.Fatalf("tokens = %d, want 1", len(user.Tokens))
	}
}
