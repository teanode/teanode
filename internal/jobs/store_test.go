package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/configs"
)

// withJobsDir sets the config directory to a temp dir, ensures the jobs
// subdirectory exists, and returns a Store pointing at it.
func withJobsDir(t *testing.T) *Store {
	t.Helper()
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	jobsDirectory := filepath.Join(directory, "jobs")
	if err := os.MkdirAll(jobsDirectory, 0755); err != nil {
		t.Fatalf("creating jobs directory: %v", err)
	}

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// sampleJob returns a minimal Job for testing.
// AgentID and ConversationID are pre-set so executeJob won't hit nil AgentRegistry.
func sampleJob(id string, name string) Job {
	return Job{
		ID:             id,
		Name:           name,
		Schedule:       "0 9 * * 1-5",
		Message:        "Hello from " + name,
		AgentID:        "main",
		ConversationID: "conv-test",
		Enabled:        true,
		CreatedAt:      1700000000000,
	}
}

// --- 1. NewStore ---

func TestNewStore(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore error: %v", err)
	}
	if store == nil {
		t.Fatal("NewStore returned nil")
	}
}

// --- 2. Load: empty / non-existent directory ---

func TestLoad_EmptyDirectory(t *testing.T) {
	store := withJobsDir(t)

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestLoad_NonExistentDirectory(t *testing.T) {
	directory := t.TempDir()
	// Point to a directory that does NOT have a "jobs" subdirectory.
	store := &Store{directory: filepath.Join(directory, "nonexistent", "jobs")}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if jobs != nil {
		t.Errorf("expected nil for non-existent directory, got %v", jobs)
	}
}

// --- 3. Create + Load round-trip ---

func TestCreate_And_Load(t *testing.T) {
	store := withJobsDir(t)

	job := sampleJob("job-alpha", "Alpha")
	job.Model = "anthropic:claude-4"
	job.AgentID = "main"
	job.OneShot = true
	job.RunAt = 1700000060000
	job.LastRun = 1700000050000
	job.LastStatus = "success"
	job.ConversationID = "conv-123"

	if err := store.Create(job); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	loaded := jobs[0]
	if loaded.ID != "job-alpha" {
		t.Errorf("ID = %q, want job-alpha", loaded.ID)
	}
	if loaded.Name != "Alpha" {
		t.Errorf("Name = %q, want Alpha", loaded.Name)
	}
	if loaded.Schedule != "0 9 * * 1-5" {
		t.Errorf("Schedule = %q, want '0 9 * * 1-5'", loaded.Schedule)
	}
	if loaded.Message != "Hello from Alpha" {
		t.Errorf("Message = %q, want 'Hello from Alpha'", loaded.Message)
	}
	if loaded.Model != "anthropic:claude-4" {
		t.Errorf("Model = %q, want anthropic:claude-4", loaded.Model)
	}
	if loaded.AgentID != "main" {
		t.Errorf("AgentID = %q, want main", loaded.AgentID)
	}
	if !loaded.Enabled {
		t.Error("Enabled = false, want true")
	}
	if !loaded.OneShot {
		t.Error("OneShot = false, want true")
	}
	if loaded.RunAt != 1700000060000 {
		t.Errorf("RunAt = %d, want 1700000060000", loaded.RunAt)
	}
	if loaded.LastRun != 1700000050000 {
		t.Errorf("LastRun = %d, want 1700000050000", loaded.LastRun)
	}
	if loaded.LastStatus != "success" {
		t.Errorf("LastStatus = %q, want success", loaded.LastStatus)
	}
	if loaded.ConversationID != "conv-123" {
		t.Errorf("ConversationID = %q, want conv-123", loaded.ConversationID)
	}
	if loaded.CreatedAt != 1700000000000 {
		t.Errorf("CreatedAt = %d, want 1700000000000", loaded.CreatedAt)
	}
}

// --- 4. Create multiple + Load returns all ---

func TestCreate_Multiple(t *testing.T) {
	store := withJobsDir(t)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := store.Create(sampleJob("job-"+name, name)); err != nil {
			t.Fatalf("Create(%s) error: %v", name, err)
		}
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("expected 3 jobs, got %d", len(jobs))
	}

	ids := map[string]bool{}
	for _, job := range jobs {
		ids[job.ID] = true
	}
	for _, expected := range []string{"job-alpha", "job-beta", "job-gamma"} {
		if !ids[expected] {
			t.Errorf("missing job ID %q", expected)
		}
	}
}

// --- 5. Update ---

func TestUpdate(t *testing.T) {
	store := withJobsDir(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := store.Create(job); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	job.Name = "Alpha Updated"
	job.Message = "Updated message"
	job.Enabled = false
	if err := store.Update(job); err != nil {
		t.Fatalf("Update error: %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Name != "Alpha Updated" {
		t.Errorf("Name = %q, want 'Alpha Updated'", jobs[0].Name)
	}
	if jobs[0].Message != "Updated message" {
		t.Errorf("Message = %q, want 'Updated message'", jobs[0].Message)
	}
	if jobs[0].Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestUpdate_NotFound(t *testing.T) {
	store := withJobsDir(t)

	job := sampleJob("nonexistent", "Ghost")
	err := store.Update(job)
	if err == nil {
		t.Fatal("expected error for updating nonexistent job, got nil")
	}
	if !strings.Contains(err.Error(), "job not found") {
		t.Errorf("error = %q, want to contain 'job not found'", err.Error())
	}
}

// --- 6. Delete ---

func TestDelete(t *testing.T) {
	store := withJobsDir(t)

	if err := store.Create(sampleJob("job-alpha", "Alpha")); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if err := store.Create(sampleJob("job-beta", "Beta")); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if err := store.Delete("job-alpha"); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after delete, got %d", len(jobs))
	}
	if jobs[0].ID != "job-beta" {
		t.Errorf("remaining job ID = %q, want job-beta", jobs[0].ID)
	}
}

func TestDelete_NotFound(t *testing.T) {
	store := withJobsDir(t)

	err := store.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent job, got nil")
	}
	if !strings.Contains(err.Error(), "job not found") {
		t.Errorf("error = %q, want to contain 'job not found'", err.Error())
	}
}

// --- 7. Save (bulk replace) ---

func TestSave_ReplacesAll(t *testing.T) {
	store := withJobsDir(t)

	// Create two jobs individually.
	if err := store.Create(sampleJob("job-alpha", "Alpha")); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if err := store.Create(sampleJob("job-beta", "Beta")); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Replace with a single different job.
	replacement := sampleJob("job-gamma", "Gamma")
	if err := store.Save([]Job{replacement}); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after Save, got %d", len(jobs))
	}
	if jobs[0].ID != "job-gamma" {
		t.Errorf("job ID = %q, want job-gamma", jobs[0].ID)
	}
}

func TestSave_EmptyList(t *testing.T) {
	store := withJobsDir(t)

	if err := store.Create(sampleJob("job-alpha", "Alpha")); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if err := store.Save([]Job{}); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after saving empty list, got %d", len(jobs))
	}
}

// --- 8. Load skips non-.md files and directories ---

func TestLoad_SkipsNonMarkdownFiles(t *testing.T) {
	store := withJobsDir(t)

	// Create a valid job.
	if err := store.Create(sampleJob("job-alpha", "Alpha")); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Add a non-.md file and a subdirectory.
	if err := os.WriteFile(filepath.Join(store.directory, "notes.txt"), []byte("not a job"), 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(store.directory, "subdir"), 0755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}

	jobs, err := store.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job (skip non-.md), got %d", len(jobs))
	}
}

// --- 9. Markdown parsing ---

func TestParseJobMarkdown_Valid(t *testing.T) {
	content := "---\nname: Test Job\nschedule: \"*/5 * * * *\"\nenabled: true\ncreatedAt: 1700000000000\n---\n\nDo the thing."
	job, err := parseJobMarkdown("test-id", []byte(content))
	if err != nil {
		t.Fatalf("parseJobMarkdown error: %v", err)
	}
	if job.ID != "test-id" {
		t.Errorf("ID = %q, want test-id", job.ID)
	}
	if job.Name != "Test Job" {
		t.Errorf("Name = %q, want 'Test Job'", job.Name)
	}
	if job.Schedule != "*/5 * * * *" {
		t.Errorf("Schedule = %q, want '*/5 * * * *'", job.Schedule)
	}
	if !job.Enabled {
		t.Error("Enabled = false, want true")
	}
	if job.Message != "Do the thing." {
		t.Errorf("Message = %q, want 'Do the thing.'", job.Message)
	}
}

func TestParseJobMarkdown_NoBody(t *testing.T) {
	content := "---\nname: Empty Body\nenabled: false\ncreatedAt: 0\n---"
	job, err := parseJobMarkdown("no-body", []byte(content))
	if err != nil {
		t.Fatalf("parseJobMarkdown error: %v", err)
	}
	if job.Message != "" {
		t.Errorf("Message = %q, want empty", job.Message)
	}
	if job.Name != "Empty Body" {
		t.Errorf("Name = %q, want 'Empty Body'", job.Name)
	}
}

func TestParseJobMarkdown_MissingOpeningDelimiter(t *testing.T) {
	content := "name: bad\n---"
	_, err := parseJobMarkdown("bad", []byte(content))
	if err == nil {
		t.Fatal("expected error for missing opening delimiter")
	}
}

func TestParseJobMarkdown_MissingClosingDelimiter(t *testing.T) {
	content := "---\nname: bad\n"
	_, err := parseJobMarkdown("bad", []byte(content))
	if err == nil {
		t.Fatal("expected error for missing closing delimiter")
	}
}

// --- 10. Markdown formatting round-trip ---

func TestFormatAndParseJobMarkdown_RoundTrip(t *testing.T) {
	original := Job{
		ID:             "roundtrip",
		Name:           "Round Trip Job",
		Schedule:       "30 12 * * *",
		Message:        "Multi-line\nmessage body\nwith content.",
		Model:          "openai:gpt-5",
		AgentID:        "main",
		Enabled:        true,
		ConversationID: "conv-abc",
		RunAt:          1700000060000,
		OneShot:        true,
		LastRun:        1700000050000,
		LastStatus:     "error",
		LastError:      "something broke",
		CreatedAt:      1700000000000,
	}

	data := formatJobMarkdown(original)
	parsed, err := parseJobMarkdown("roundtrip", data)
	if err != nil {
		t.Fatalf("parseJobMarkdown error: %v", err)
	}

	if parsed.ID != original.ID {
		t.Errorf("ID = %q, want %q", parsed.ID, original.ID)
	}
	if parsed.Name != original.Name {
		t.Errorf("Name = %q, want %q", parsed.Name, original.Name)
	}
	if parsed.Schedule != original.Schedule {
		t.Errorf("Schedule = %q, want %q", parsed.Schedule, original.Schedule)
	}
	if parsed.Message != original.Message {
		t.Errorf("Message = %q, want %q", parsed.Message, original.Message)
	}
	if parsed.Model != original.Model {
		t.Errorf("Model = %q, want %q", parsed.Model, original.Model)
	}
	if parsed.AgentID != original.AgentID {
		t.Errorf("AgentID = %q, want %q", parsed.AgentID, original.AgentID)
	}
	if parsed.Enabled != original.Enabled {
		t.Errorf("Enabled = %v, want %v", parsed.Enabled, original.Enabled)
	}
	if parsed.ConversationID != original.ConversationID {
		t.Errorf("ConversationID = %q, want %q", parsed.ConversationID, original.ConversationID)
	}
	if parsed.RunAt != original.RunAt {
		t.Errorf("RunAt = %d, want %d", parsed.RunAt, original.RunAt)
	}
	if parsed.OneShot != original.OneShot {
		t.Errorf("OneShot = %v, want %v", parsed.OneShot, original.OneShot)
	}
	if parsed.LastRun != original.LastRun {
		t.Errorf("LastRun = %d, want %d", parsed.LastRun, original.LastRun)
	}
	if parsed.LastStatus != original.LastStatus {
		t.Errorf("LastStatus = %q, want %q", parsed.LastStatus, original.LastStatus)
	}
	if parsed.LastError != original.LastError {
		t.Errorf("LastError = %q, want %q", parsed.LastError, original.LastError)
	}
	if parsed.CreatedAt != original.CreatedAt {
		t.Errorf("CreatedAt = %d, want %d", parsed.CreatedAt, original.CreatedAt)
	}
}
