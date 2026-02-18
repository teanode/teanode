package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestNewStore(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.directory != directory {
		t.Errorf("directory = %q, want %q", store.directory, directory)
	}
}

func TestCreate(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "sessions")
	store := NewStore(directory)

	session, err := store.Create("Mozilla/5.0", "192.168.1.1", 24*time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.UserAgent != "Mozilla/5.0" {
		t.Errorf("UserAgent = %q, want %q", session.UserAgent, "Mozilla/5.0")
	}
	if session.RemoteAddr != "192.168.1.1" {
		t.Errorf("RemoteAddr = %q, want %q", session.RemoteAddr, "192.168.1.1")
	}
	if session.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if session.LastSeenAt.IsZero() {
		t.Error("expected non-zero LastSeenAt")
	}

	// ExpiresAt should be approximately 24 hours from now.
	expectedExpiry := time.Now().Add(24 * time.Hour)
	if session.ExpiresAt.Before(expectedExpiry.Add(-time.Minute)) || session.ExpiresAt.After(expectedExpiry.Add(time.Minute)) {
		t.Errorf("ExpiresAt = %v, expected ~%v", session.ExpiresAt, expectedExpiry)
	}

	// Verify the session was persisted to disk.
	sessionPath := filepath.Join(directory, session.ID+".yaml")
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("reading session file: %v", err)
	}
	var persisted Session
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshalling session file: %v", err)
	}
	if persisted.ID != session.ID {
		t.Errorf("persisted ID = %q, want %q", persisted.ID, session.ID)
	}
	if persisted.UserAgent != session.UserAgent {
		t.Errorf("persisted UserAgent = %q, want %q", persisted.UserAgent, session.UserAgent)
	}
}

func TestCreate_MultipleSessionsHaveUniqueIDs(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("agent-1", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create("agent-2", "10.0.0.2", time.Hour)
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	if first.ID == second.ID {
		t.Errorf("expected unique IDs, both are %q", first.ID)
	}
}

func TestGet_ExistingSession(t *testing.T) {
	store := NewStore(t.TempDir())

	created, err := store.Create("Chrome", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	retrieved := store.Get(created.ID)
	if retrieved == nil {
		t.Fatal("expected non-nil session")
	}
	if retrieved.ID != created.ID {
		t.Errorf("ID = %q, want %q", retrieved.ID, created.ID)
	}
	if retrieved.UserAgent != created.UserAgent {
		t.Errorf("UserAgent = %q, want %q", retrieved.UserAgent, created.UserAgent)
	}
	if retrieved.RemoteAddr != created.RemoteAddr {
		t.Errorf("RemoteAddr = %q, want %q", retrieved.RemoteAddr, created.RemoteAddr)
	}
}

func TestGet_MissingSession(t *testing.T) {
	store := NewStore(t.TempDir())

	retrieved := store.Get("nonexistent-id")
	if retrieved != nil {
		t.Errorf("expected nil for missing session, got %+v", retrieved)
	}
}

func TestGet_ExpiredSession(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a session that's already expired by writing it directly.
	expired := &Session{
		ID:         "expired-session",
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		ExpiresAt:  time.Now().Add(-1 * time.Hour),
		UserAgent:  "Old Browser",
		RemoteAddr: "10.0.0.1",
		LastSeenAt: time.Now().Add(-2 * time.Hour),
	}
	data, err := yaml.Marshal(expired)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sessionPath := filepath.Join(directory, expired.ID+".yaml")
	if err := os.WriteFile(sessionPath, data, 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	// Get should return nil for expired sessions.
	retrieved := store.Get("expired-session")
	if retrieved != nil {
		t.Errorf("expected nil for expired session, got %+v", retrieved)
	}

	// The expired session file should have been cleaned up.
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("expected expired session file to be removed")
	}
}

func TestGet_ValidNonExpiredSession(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a session with future expiry.
	session := &Session{
		ID:         "valid-session",
		CreatedAt:  time.Now().Add(-30 * time.Minute),
		ExpiresAt:  time.Now().Add(30 * time.Minute),
		UserAgent:  "Firefox",
		RemoteAddr: "10.0.0.2",
		LastSeenAt: time.Now().Add(-30 * time.Minute),
	}
	data, err := yaml.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, session.ID+".yaml"), data, 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	retrieved := store.Get("valid-session")
	if retrieved == nil {
		t.Fatal("expected non-nil session for valid non-expired session")
	}
	if retrieved.ID != "valid-session" {
		t.Errorf("ID = %q, want %q", retrieved.ID, "valid-session")
	}
}

func TestTouch_UpdatesTimestamps(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Write a session with LastSeenAt more than an hour ago to bypass throttle.
	session := &Session{
		ID:         "touch-me",
		CreatedAt:  time.Now().Add(-3 * time.Hour),
		ExpiresAt:  time.Now().Add(1 * time.Hour),
		UserAgent:  "Safari",
		RemoteAddr: "10.0.0.3",
		LastSeenAt: time.Now().Add(-2 * time.Hour),
	}
	data, err := yaml.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, session.ID+".yaml"), data, 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	originalExpiry := session.ExpiresAt

	store.Touch("touch-me", 24*time.Hour)

	// Re-read the session from disk.
	updated := store.Get("touch-me")
	if updated == nil {
		t.Fatal("expected non-nil session after Touch")
	}

	// LastSeenAt should be updated to approximately now.
	if time.Since(updated.LastSeenAt) > time.Minute {
		t.Errorf("LastSeenAt = %v, expected approximately now", updated.LastSeenAt)
	}

	// ExpiresAt should be extended beyond the original.
	if !updated.ExpiresAt.After(originalExpiry) {
		t.Errorf("ExpiresAt = %v should be after original %v", updated.ExpiresAt, originalExpiry)
	}
}

func TestTouch_ThrottledWithinOneHour(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a session with LastSeenAt very recent (less than an hour ago).
	session := &Session{
		ID:         "recent-session",
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		ExpiresAt:  time.Now().Add(50 * time.Minute),
		UserAgent:  "Edge",
		RemoteAddr: "10.0.0.4",
		LastSeenAt: time.Now().Add(-10 * time.Minute),
	}
	data, err := yaml.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, session.ID+".yaml"), data, 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	originalLastSeen := session.LastSeenAt
	originalExpiry := session.ExpiresAt

	store.Touch("recent-session", 24*time.Hour)

	// Re-read — should not have been updated because of throttling.
	retrieved := store.Get("recent-session")
	if retrieved == nil {
		t.Fatal("expected non-nil session")
	}

	if !retrieved.LastSeenAt.Equal(originalLastSeen) {
		t.Errorf("LastSeenAt = %v, expected unchanged %v (throttled)", retrieved.LastSeenAt, originalLastSeen)
	}
	if !retrieved.ExpiresAt.Equal(originalExpiry) {
		t.Errorf("ExpiresAt = %v, expected unchanged %v (throttled)", retrieved.ExpiresAt, originalExpiry)
	}
}

func TestTouch_NonexistentSession(t *testing.T) {
	store := NewStore(t.TempDir())

	// Should not panic on nonexistent session.
	store.Touch("does-not-exist", time.Hour)
}

func TestDelete_ExistingSession(t *testing.T) {
	store := NewStore(t.TempDir())

	session, err := store.Create("Chrome", "127.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(session.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Session should no longer be retrievable.
	retrieved := store.Get(session.ID)
	if retrieved != nil {
		t.Errorf("expected nil after Delete, got %+v", retrieved)
	}
}

func TestDelete_NonexistentSession(t *testing.T) {
	store := NewStore(t.TempDir())

	err := store.Delete("nonexistent-id")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent session")
	}
}

func TestList_Empty(t *testing.T) {
	store := NewStore(t.TempDir())

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestList_NonexistentDirectory(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "nonexistent"))

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil for nonexistent directory, got %v", sessions)
	}
}

func TestList_MultipleSessions(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("Agent-A", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create("Agent-B", "10.0.0.2", time.Hour)
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Both session IDs should be present.
	ids := map[string]bool{}
	for _, session := range sessions {
		ids[session.ID] = true
	}
	if !ids[first.ID] {
		t.Errorf("session list missing ID %q", first.ID)
	}
	if !ids[second.ID] {
		t.Errorf("session list missing ID %q", second.ID)
	}
}

func TestList_FiltersExpiredSessions(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a valid session via the store.
	valid, err := store.Create("Valid", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write an expired session directly to disk.
	expired := &Session{
		ID:         "expired-list-test",
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		ExpiresAt:  time.Now().Add(-1 * time.Hour),
		UserAgent:  "Expired",
		RemoteAddr: "10.0.0.2",
		LastSeenAt: time.Now().Add(-2 * time.Hour),
	}
	data, err := yaml.Marshal(expired)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, expired.ID+".yaml"), data, 0644); err != nil {
		t.Fatalf("write expired session: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session (expired filtered out), got %d", len(sessions))
	}
	if sessions[0].ID != valid.ID {
		t.Errorf("session ID = %q, want %q", sessions[0].ID, valid.ID)
	}

	// The expired session file should have been cleaned up.
	expiredPath := filepath.Join(directory, expired.ID+".yaml")
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Error("expected expired session file to be removed by List")
	}
}

func TestList_SkipsNonYAMLFiles(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a valid session.
	_, err := store.Create("Valid", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create a non-YAML file in the directory.
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("not a session"), 0644); err != nil {
		t.Fatalf("write non-yaml file: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (non-yaml skipped), got %d", len(sessions))
	}
}

func TestList_SkipsSubdirectories(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	_, err := store.Create("Valid", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create a subdirectory.
	if err := os.Mkdir(filepath.Join(directory, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (subdir skipped), got %d", len(sessions))
	}
}

func TestList_SkipsMalformedYAML(t *testing.T) {
	directory := t.TempDir()
	store := NewStore(directory)

	_, err := store.Create("Valid", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write a malformed YAML file that looks like a session.
	if err := os.WriteFile(filepath.Join(directory, "bad-session.yaml"), []byte("not: [valid: yaml: {{"), 0644); err != nil {
		t.Fatalf("write malformed yaml: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (malformed skipped), got %d", len(sessions))
	}
}

func TestDelete_ThenList(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("Agent-A", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create("Agent-B", "10.0.0.2", time.Hour)
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}

	if err := store.Delete(first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after deletion, got %d", len(sessions))
	}
	if sessions[0].ID != second.ID {
		t.Errorf("remaining session ID = %q, want %q", sessions[0].ID, second.ID)
	}
}

func TestSessionPath(t *testing.T) {
	store := NewStore("/tmp/sessions")
	path := store.sessionPath("abc123")
	expected := "/tmp/sessions/abc123.yaml"
	if path != expected {
		t.Errorf("sessionPath = %q, want %q", path, expected)
	}
}
