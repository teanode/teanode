package jobs

import "testing"

// --- 1. toJob conversion ---

func TestJobFrontmatter_ToJob(t *testing.T) {
	frontmatter := jobFrontmatter{
		Name:           "Test Job",
		Schedule:       "0 9 * * *",
		Model:          "openai:gpt-5",
		AgentID:        "main",
		Enabled:        true,
		ConversationID: "conv-123",
		RunAt:          1700000060000,
		OneShot:        true,
		LastRun:        1700000050000,
		LastStatus:     "success",
		LastError:      "",
		CreatedAt:      1700000000000,
	}

	job := frontmatter.toJob("my-id", "the message body")

	if job.ID != "my-id" {
		t.Errorf("ID = %q, want my-id", job.ID)
	}
	if job.Name != "Test Job" {
		t.Errorf("Name = %q, want 'Test Job'", job.Name)
	}
	if job.Schedule != "0 9 * * *" {
		t.Errorf("Schedule = %q, want '0 9 * * *'", job.Schedule)
	}
	if job.Message != "the message body" {
		t.Errorf("Message = %q, want 'the message body'", job.Message)
	}
	if job.Model != "openai:gpt-5" {
		t.Errorf("Model = %q, want openai:gpt-5", job.Model)
	}
	if job.AgentID != "main" {
		t.Errorf("AgentID = %q, want main", job.AgentID)
	}
	if !job.Enabled {
		t.Error("Enabled = false, want true")
	}
	if job.ConversationID != "conv-123" {
		t.Errorf("ConversationID = %q, want conv-123", job.ConversationID)
	}
	if job.RunAt != 1700000060000 {
		t.Errorf("RunAt = %d, want 1700000060000", job.RunAt)
	}
	if !job.OneShot {
		t.Error("OneShot = false, want true")
	}
	if job.LastRun != 1700000050000 {
		t.Errorf("LastRun = %d, want 1700000050000", job.LastRun)
	}
	if job.LastStatus != "success" {
		t.Errorf("LastStatus = %q, want success", job.LastStatus)
	}
	if job.LastError != "" {
		t.Errorf("LastError = %q, want empty", job.LastError)
	}
	if job.CreatedAt != 1700000000000 {
		t.Errorf("CreatedAt = %d, want 1700000000000", job.CreatedAt)
	}
}

// --- 2. toFrontmatter conversion ---

func TestToFrontmatter(t *testing.T) {
	job := Job{
		ID:             "my-id",
		Name:           "Test Job",
		Schedule:       "0 9 * * *",
		Message:        "the message body",
		Model:          "openai:gpt-5",
		AgentID:        "main",
		Enabled:        true,
		ConversationID: "conv-123",
		RunAt:          1700000060000,
		OneShot:        true,
		LastRun:        1700000050000,
		LastStatus:     "error",
		LastError:      "something broke",
		CreatedAt:      1700000000000,
	}

	frontmatter := toFrontmatter(job)

	// ID and Message should NOT be in frontmatter (stored separately).
	if frontmatter.Name != job.Name {
		t.Errorf("Name = %q, want %q", frontmatter.Name, job.Name)
	}
	if frontmatter.Schedule != job.Schedule {
		t.Errorf("Schedule = %q, want %q", frontmatter.Schedule, job.Schedule)
	}
	if frontmatter.Model != job.Model {
		t.Errorf("Model = %q, want %q", frontmatter.Model, job.Model)
	}
	if frontmatter.AgentID != job.AgentID {
		t.Errorf("AgentID = %q, want %q", frontmatter.AgentID, job.AgentID)
	}
	if frontmatter.Enabled != job.Enabled {
		t.Errorf("Enabled = %v, want %v", frontmatter.Enabled, job.Enabled)
	}
	if frontmatter.ConversationID != job.ConversationID {
		t.Errorf("ConversationID = %q, want %q", frontmatter.ConversationID, job.ConversationID)
	}
	if frontmatter.RunAt != job.RunAt {
		t.Errorf("RunAt = %d, want %d", frontmatter.RunAt, job.RunAt)
	}
	if frontmatter.OneShot != job.OneShot {
		t.Errorf("OneShot = %v, want %v", frontmatter.OneShot, job.OneShot)
	}
	if frontmatter.LastRun != job.LastRun {
		t.Errorf("LastRun = %d, want %d", frontmatter.LastRun, job.LastRun)
	}
	if frontmatter.LastStatus != job.LastStatus {
		t.Errorf("LastStatus = %q, want %q", frontmatter.LastStatus, job.LastStatus)
	}
	if frontmatter.LastError != job.LastError {
		t.Errorf("LastError = %q, want %q", frontmatter.LastError, job.LastError)
	}
	if frontmatter.CreatedAt != job.CreatedAt {
		t.Errorf("CreatedAt = %d, want %d", frontmatter.CreatedAt, job.CreatedAt)
	}
}

// --- 3. Round-trip: Job -> frontmatter -> Job ---

func TestFrontmatterRoundTrip(t *testing.T) {
	original := Job{
		ID:             "rt-id",
		Name:           "Roundtrip",
		Schedule:       "*/10 * * * *",
		Message:        "ping",
		Model:          "anthropic:claude-4",
		AgentID:        "agent-x",
		Enabled:        false,
		ConversationID: "conv-rt",
		RunAt:          999,
		OneShot:        false,
		LastRun:        888,
		LastStatus:     "success",
		LastError:      "",
		CreatedAt:      777,
	}

	frontmatter := toFrontmatter(original)
	rebuilt := frontmatter.toJob(original.ID, original.Message)

	if rebuilt != original {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", rebuilt, original)
	}
}
