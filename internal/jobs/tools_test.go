package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/configs"
)

// newTestTool creates a jobsTool with a scheduler backed by a temp-dir Store.
func newTestTool(t *testing.T) *jobsTool {
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
	scheduler := NewScheduler(store, nil)

	// Wire up a no-op RunMessage so Trigger works.
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	return &jobsTool{scheduler: scheduler}
}

// --- 1. truncateString ---

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		maxLength int
		expected  string
	}{
		{"under limit", "hello", 10, "hello"},
		{"at limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello..."},
		{"empty string", "", 5, ""},
		{"zero max", "hello", 0, "..."},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := truncateString(testCase.value, testCase.maxLength)
			if result != testCase.expected {
				t.Errorf("truncateString(%q, %d) = %q, want %q", testCase.value, testCase.maxLength, result, testCase.expected)
			}
		})
	}
}

// --- 2. Definition ---

func TestJobsTool_Definition(t *testing.T) {
	tool := newTestTool(t)
	definition := tool.Definition()

	if definition.Function.Name != "jobs" {
		t.Errorf("Name = %q, want jobs", definition.Function.Name)
	}
	if definition.Type != "function" {
		t.Errorf("Type = %q, want function", definition.Type)
	}
	if definition.Function.Returns == nil {
		t.Error("Returns schema is nil")
	}
}

// --- 3. Execute: list ---

func TestExecute_List_Empty(t *testing.T) {
	tool := newTestTool(t)

	result, err := tool.Execute(context.Background(), `{"action":"list"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["action"] != "list" {
		t.Errorf("action = %v, want list", parsed["action"])
	}
	jobsList, ok := parsed["jobs"].([]interface{})
	if !ok {
		t.Fatalf("jobs is not an array: %T", parsed["jobs"])
	}
	if len(jobsList) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobsList))
	}
}

func TestExecute_List_WithJobs(t *testing.T) {
	tool := newTestTool(t)

	// Create a job directly in the store.
	job := sampleJob("job-alpha", "Alpha")
	if err := tool.scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	result, err := tool.Execute(context.Background(), `{"action":"list"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	jobsList := parsed["jobs"].([]interface{})
	if len(jobsList) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobsList))
	}
}

// --- 4. Execute: create with schedule ---

func TestExecute_Create_WithSchedule(t *testing.T) {
	tool := newTestTool(t)

	arguments := `{"action":"create","name":"Morning Report","schedule":"0 9 * * 1-5","message":"Generate report"}`
	result, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["action"] != "create" {
		t.Errorf("action = %v, want create", parsed["action"])
	}
	if parsed["name"] != "Morning Report" {
		t.Errorf("name = %v, want 'Morning Report'", parsed["name"])
	}
	if parsed["id"] == nil || parsed["id"] == "" {
		t.Error("id should be set")
	}
	if parsed["schedule"] != "0 9 * * 1-5" {
		t.Errorf("schedule = %v, want '0 9 * * 1-5'", parsed["schedule"])
	}

	// Verify job was persisted.
	jobs := tool.scheduler.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job in scheduler, got %d", len(jobs))
	}
	if jobs[0].Name != "Morning Report" {
		t.Errorf("persisted Name = %q, want 'Morning Report'", jobs[0].Name)
	}
	if !jobs[0].Enabled {
		t.Error("new job should be enabled")
	}
}

// --- 5. Execute: create with delay ---

func TestExecute_Create_WithDelay(t *testing.T) {
	tool := newTestTool(t)

	arguments := `{"action":"create","name":"Reminder","delay":"1h","message":"Check in"}`
	result, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["firesAt"] == nil {
		t.Error("firesAt should be set for delay jobs")
	}

	jobs := tool.scheduler.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if !jobs[0].OneShot {
		t.Error("delay jobs should be OneShot")
	}
	if jobs[0].RunAt == 0 {
		t.Error("RunAt should be set for delay jobs")
	}
	// RunAt should be roughly 1 hour from now.
	expectedRunAt := time.Now().Add(time.Hour).UnixMilli()
	tolerance := int64(5000) // 5 seconds tolerance
	if jobs[0].RunAt < expectedRunAt-tolerance || jobs[0].RunAt > expectedRunAt+tolerance {
		t.Errorf("RunAt = %d, expected roughly %d", jobs[0].RunAt, expectedRunAt)
	}
}

// --- 6. Execute: create validation errors ---

func TestExecute_Create_MissingName(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","schedule":"* * * * *","message":"hello"}`)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name and message are required") {
		t.Errorf("error = %q, want to contain 'name and message are required'", err.Error())
	}
}

func TestExecute_Create_MissingMessage(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","name":"test","schedule":"* * * * *"}`)
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestExecute_Create_BothScheduleAndDelay(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","name":"test","message":"hi","schedule":"* * * * *","delay":"1h"}`)
	if err == nil {
		t.Fatal("expected error for both schedule and delay")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Errorf("error = %q, want to contain 'not both'", err.Error())
	}
}

func TestExecute_Create_NeitherScheduleNorDelay(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","name":"test","message":"hi"}`)
	if err == nil {
		t.Fatal("expected error for neither schedule nor delay")
	}
}

func TestExecute_Create_InvalidSchedule(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","name":"test","message":"hi","schedule":"not valid"}`)
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
	if !strings.Contains(err.Error(), "invalid schedule") {
		t.Errorf("error = %q, want to contain 'invalid schedule'", err.Error())
	}
}

func TestExecute_Create_InvalidDelay(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","name":"test","message":"hi","delay":"not-a-duration"}`)
	if err == nil {
		t.Fatal("expected error for invalid delay")
	}
	if !strings.Contains(err.Error(), "invalid delay") {
		t.Errorf("error = %q, want to contain 'invalid delay'", err.Error())
	}
}

func TestExecute_Create_DelayTooShort(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"create","name":"test","message":"hi","delay":"30s"}`)
	if err == nil {
		t.Fatal("expected error for delay < 1 minute")
	}
	if !strings.Contains(err.Error(), "at least 1 minute") {
		t.Errorf("error = %q, want to contain 'at least 1 minute'", err.Error())
	}
}

// --- 7. Execute: update ---

func TestExecute_Update(t *testing.T) {
	tool := newTestTool(t)

	// Create a job first.
	job := sampleJob("job-alpha", "Alpha")
	if err := tool.scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	arguments := `{"action":"update","id":"job-alpha","name":"Alpha Updated","message":"new message"}`
	result, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["success"] != true {
		t.Errorf("success = %v, want true", parsed["success"])
	}

	jobs := tool.scheduler.List()
	if jobs[0].Name != "Alpha Updated" {
		t.Errorf("Name = %q, want 'Alpha Updated'", jobs[0].Name)
	}
}

func TestExecute_Update_MissingID(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"update","name":"test"}`)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("error = %q, want to contain 'id is required'", err.Error())
	}
}

func TestExecute_Update_NotFound(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"update","id":"nonexistent","name":"test"}`)
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
	if !strings.Contains(err.Error(), "job not found") {
		t.Errorf("error = %q, want to contain 'job not found'", err.Error())
	}
}

func TestExecute_Update_EnableDisable(t *testing.T) {
	tool := newTestTool(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := tool.scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	// Disable the job.
	_, err := tool.Execute(context.Background(), `{"action":"update","id":"job-alpha","enabled":false}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	jobs := tool.scheduler.List()
	if jobs[0].Enabled {
		t.Error("job should be disabled")
	}

	// Re-enable.
	_, err = tool.Execute(context.Background(), `{"action":"update","id":"job-alpha","enabled":true}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	jobs = tool.scheduler.List()
	if !jobs[0].Enabled {
		t.Error("job should be enabled")
	}
}

func TestExecute_Update_InvalidSchedule(t *testing.T) {
	tool := newTestTool(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := tool.scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	_, err := tool.Execute(context.Background(), `{"action":"update","id":"job-alpha","schedule":"bad cron"}`)
	if err == nil {
		t.Fatal("expected error for invalid schedule")
	}
}

// --- 8. Execute: delete ---

func TestExecute_Delete(t *testing.T) {
	tool := newTestTool(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := tool.scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	result, err := tool.Execute(context.Background(), `{"action":"delete","id":"job-alpha"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["success"] != true {
		t.Errorf("success = %v, want true", parsed["success"])
	}

	jobs := tool.scheduler.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after delete, got %d", len(jobs))
	}
}

func TestExecute_Delete_MissingID(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"delete"}`)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestExecute_Delete_NotFound(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"delete","id":"nonexistent"}`)
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

// --- 9. Execute: trigger ---

func TestExecute_Trigger(t *testing.T) {
	tool := newTestTool(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := tool.scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	triggered := false
	tool.scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		triggered = true
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	result, err := tool.Execute(context.Background(), `{"action":"trigger","id":"job-alpha"}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["success"] != true {
		t.Errorf("success = %v, want true", parsed["success"])
	}

	// Wait for goroutine.
	time.Sleep(200 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if !triggered {
		t.Error("job should have been triggered")
	}
}

func TestExecute_Trigger_MissingID(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"trigger"}`)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestExecute_Trigger_NotFound(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"trigger","id":"nonexistent"}`)
	if err == nil {
		t.Fatal("expected error for nonexistent job")
	}
}

// --- 10. Execute: unknown action ---

func TestExecute_UnknownAction(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{"action":"frobnicate"}`)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown jobs action") {
		t.Errorf("error = %q, want to contain 'unknown jobs action'", err.Error())
	}
}

// --- 11. Execute: invalid JSON ---

func TestExecute_InvalidJSON(t *testing.T) {
	tool := newTestTool(t)

	_, err := tool.Execute(context.Background(), `{bad json}`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- 12. Create with model and agentId ---

func TestExecute_Create_WithModelAndAgent(t *testing.T) {
	tool := newTestTool(t)

	arguments := `{"action":"create","name":"Special","schedule":"0 * * * *","message":"do it","model":"anthropic:claude-4","agentId":"research"}`
	_, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	jobs := tool.scheduler.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Model != "anthropic:claude-4" {
		t.Errorf("Model = %q, want anthropic:claude-4", jobs[0].Model)
	}
	if jobs[0].AgentID != "research" {
		t.Errorf("AgentID = %q, want research", jobs[0].AgentID)
	}
}

// --- 13. Create with explicit oneShot override ---

func TestExecute_Create_OneShotOverride(t *testing.T) {
	tool := newTestTool(t)

	// Create with schedule but oneShot=true.
	arguments := `{"action":"create","name":"OneTime Cron","schedule":"0 9 * * *","message":"once","oneShot":true}`
	_, err := tool.Execute(context.Background(), arguments)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	jobs := tool.scheduler.List()
	if !jobs[0].OneShot {
		t.Error("oneShot should be true when explicitly set")
	}
}
