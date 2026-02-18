package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/configs"
)

// newTestScheduler creates a Scheduler backed by a temp-dir Store
// with no agent registry (nil). Callers must wire RunMessage etc. as needed.
func newTestScheduler(t *testing.T) *Scheduler {
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
	return scheduler
}

// --- 1. NewScheduler ---

func TestNewScheduler(t *testing.T) {
	scheduler := newTestScheduler(t)
	if scheduler == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if scheduler.expressions == nil {
		t.Error("expressions map is nil")
	}
}

// --- 2. Reload / List ---

func TestReload_LoadsFromStore(t *testing.T) {
	scheduler := newTestScheduler(t)

	// Create jobs directly in the store.
	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.store.Create(job); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	jobs := scheduler.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != "job-alpha" {
		t.Errorf("job ID = %q, want job-alpha", jobs[0].ID)
	}
}

func TestReload_BuildsExpressionCache(t *testing.T) {
	scheduler := newTestScheduler(t)

	// Create an enabled cron job and a disabled one.
	enabledJob := sampleJob("job-enabled", "Enabled")
	enabledJob.Schedule = "0 9 * * *"
	enabledJob.Enabled = true

	disabledJob := sampleJob("job-disabled", "Disabled")
	disabledJob.Schedule = "0 10 * * *"
	disabledJob.Enabled = false

	oneShotJob := sampleJob("job-oneshot", "OneShot")
	oneShotJob.Schedule = ""
	oneShotJob.RunAt = time.Now().Add(time.Hour).UnixMilli()
	oneShotJob.OneShot = true
	oneShotJob.Enabled = true

	for _, job := range []Job{enabledJob, disabledJob, oneShotJob} {
		if err := scheduler.store.Create(job); err != nil {
			t.Fatalf("Create(%s) error: %v", job.ID, err)
		}
	}

	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	// Only enabled cron jobs should have cached expressions.
	if _, ok := scheduler.expressions["job-enabled"]; !ok {
		t.Error("expected expression for enabled cron job")
	}
	if _, ok := scheduler.expressions["job-disabled"]; ok {
		t.Error("disabled job should not have a cached expression")
	}
	if _, ok := scheduler.expressions["job-oneshot"]; ok {
		t.Error("one-shot (RunAt) job should not have a cached expression")
	}
}

func TestReload_SkipsBadSchedule(t *testing.T) {
	scheduler := newTestScheduler(t)

	badJob := sampleJob("job-bad", "Bad Schedule")
	badJob.Schedule = "not a cron expression"
	badJob.Enabled = true

	if err := scheduler.store.Create(badJob); err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Should not return an error — just skip the bad expression.
	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	if _, ok := scheduler.expressions["job-bad"]; ok {
		t.Error("bad schedule should not have a cached expression")
	}
}

func TestList_ReturnsCopy(t *testing.T) {
	scheduler := newTestScheduler(t)

	if err := scheduler.store.Create(sampleJob("job-alpha", "Alpha")); err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}

	list := scheduler.List()
	list[0].Name = "Mutated"

	// The scheduler's internal copy should not be affected.
	internal := scheduler.List()
	if internal[0].Name == "Mutated" {
		t.Error("List() did not return a copy — mutation leaked to internal state")
	}
}

// --- 3. CreateAndReload / UpdateAndReload / DeleteAndReload ---

func TestCreateAndReload(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	jobs := scheduler.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].ID != "job-alpha" {
		t.Errorf("job ID = %q, want job-alpha", jobs[0].ID)
	}
}

func TestUpdateAndReload(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	job.Name = "Alpha Updated"
	if err := scheduler.UpdateAndReload(job); err != nil {
		t.Fatalf("UpdateAndReload error: %v", err)
	}

	jobs := scheduler.List()
	if jobs[0].Name != "Alpha Updated" {
		t.Errorf("Name = %q, want 'Alpha Updated'", jobs[0].Name)
	}
}

func TestDeleteAndReload(t *testing.T) {
	scheduler := newTestScheduler(t)

	if err := scheduler.CreateAndReload(sampleJob("job-alpha", "Alpha")); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	if err := scheduler.DeleteAndReload("job-alpha"); err != nil {
		t.Fatalf("DeleteAndReload error: %v", err)
	}

	jobs := scheduler.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after delete, got %d", len(jobs))
	}
}

// --- 4. Trigger ---

func TestTrigger_NotFound(t *testing.T) {
	scheduler := newTestScheduler(t)

	err := scheduler.Trigger("nonexistent")
	if err == nil {
		t.Fatal("expected error for triggering nonexistent job")
	}
}

func TestTrigger_ExecutesJob(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	var triggeredMessage string
	done := make(chan struct{})

	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		triggeredMessage = message
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	if err := scheduler.Trigger("job-alpha"); err != nil {
		t.Fatalf("Trigger error: %v", err)
	}

	// Wait for the goroutine to execute.
	go func() {
		// Give the goroutine time to execute.
		time.Sleep(200 * time.Millisecond)
		close(done)
	}()
	<-done

	mutex.Lock()
	if triggeredMessage != "Hello from Alpha" {
		t.Errorf("triggered message = %q, want 'Hello from Alpha'", triggeredMessage)
	}
	mutex.Unlock()
}

// --- 5. tick() behavior ---

func TestTick_CronJobMatches(t *testing.T) {
	scheduler := newTestScheduler(t)

	// Schedule: every minute on the hour mark (i.e. "0 * * * *" means minute=0).
	// We'll use "* * * * *" to match every minute.
	job := sampleJob("job-every-minute", "Every Minute")
	job.Schedule = "* * * * *"
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	executedJobs := []string{}
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedJobs = append(executedJobs, message)
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	// Tick at any time — should match "* * * * *".
	scheduler.tick(time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC))

	// Wait for the goroutine.
	time.Sleep(200 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if len(executedJobs) != 1 {
		t.Fatalf("expected 1 executed job, got %d", len(executedJobs))
	}
	if executedJobs[0] != "Hello from Every Minute" {
		t.Errorf("message = %q, want 'Hello from Every Minute'", executedJobs[0])
	}
}

func TestTick_CronJobDoesNotMatch(t *testing.T) {
	scheduler := newTestScheduler(t)

	// Schedule: minute 0, hour 9 — only matches at 09:00.
	job := sampleJob("job-9am", "Nine AM")
	job.Schedule = "0 9 * * *"
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	executedCount := 0
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedCount++
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	// Tick at 10:30 — should NOT match "0 9 * * *".
	scheduler.tick(time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC))

	time.Sleep(100 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if executedCount != 0 {
		t.Errorf("expected 0 executions for non-matching time, got %d", executedCount)
	}
}

func TestTick_DisabledJobSkipped(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-disabled", "Disabled")
	job.Schedule = "* * * * *"
	job.Enabled = false
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	executedCount := 0
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		executedCount++
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	scheduler.tick(time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC))
	time.Sleep(100 * time.Millisecond)

	if executedCount != 0 {
		t.Errorf("disabled job should not execute, got %d executions", executedCount)
	}
}

func TestTick_OneShotRunAt_Fires(t *testing.T) {
	scheduler := newTestScheduler(t)

	// RunAt in the past relative to tick time.
	tickTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	job := sampleJob("job-oneshot", "One Shot")
	job.Schedule = ""
	job.RunAt = tickTime.Add(-time.Minute).UnixMilli() // 1 minute ago
	job.OneShot = true
	job.Enabled = true
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	var executedMessage string
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedMessage = message
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	scheduler.tick(tickTime)
	time.Sleep(300 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if executedMessage != "Hello from One Shot" {
		t.Errorf("message = %q, want 'Hello from One Shot'", executedMessage)
	}
}

func TestTick_OneShotRunAt_NotYet(t *testing.T) {
	scheduler := newTestScheduler(t)

	tickTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	job := sampleJob("job-future", "Future")
	job.Schedule = ""
	job.RunAt = tickTime.Add(time.Hour).UnixMilli() // 1 hour from now
	job.OneShot = true
	job.Enabled = true
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	executedCount := 0
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		executedCount++
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	scheduler.tick(tickTime)
	time.Sleep(100 * time.Millisecond)

	if executedCount != 0 {
		t.Errorf("future one-shot should not fire, got %d executions", executedCount)
	}
}

// --- 6. executeJob updates status ---

func TestExecuteJob_SuccessUpdatesStatus(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	scheduler.executeJob(job)

	// Reload to check persisted status.
	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	jobs := scheduler.List()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].LastStatus != "success" {
		t.Errorf("LastStatus = %q, want success", jobs[0].LastStatus)
	}
	if jobs[0].LastRun == 0 {
		t.Error("LastRun should be set after execution")
	}
	if jobs[0].LastError != "" {
		t.Errorf("LastError = %q, want empty", jobs[0].LastError)
	}
}

func TestExecuteJob_ErrorUpdatesStatus(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return fmt.Errorf("provider timeout") }
	}

	scheduler.executeJob(job)

	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	jobs := scheduler.List()
	if jobs[0].LastStatus != "error" {
		t.Errorf("LastStatus = %q, want error", jobs[0].LastStatus)
	}
	if jobs[0].LastError != "provider timeout" {
		t.Errorf("LastError = %q, want 'provider timeout'", jobs[0].LastError)
	}
}

func TestExecuteJob_OneShotDisablesAndDeletes(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-oneshot", "OneShot")
	job.OneShot = true
	job.RunAt = time.Now().Add(-time.Minute).UnixMilli()
	job.Enabled = true
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	broadcastCalled := false
	scheduler.Broadcast = func(event string, payload interface{}) {
		broadcastCalled = true
	}
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	scheduler.executeJob(job)

	// One-shot jobs should be deleted from the store after execution.
	if err := scheduler.Reload(); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	jobs := scheduler.List()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after one-shot execution, got %d", len(jobs))
	}
	if !broadcastCalled {
		t.Error("Broadcast should be called after one-shot execution")
	}
}

func TestExecuteJob_NoRunMessageCallback(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-alpha", "Alpha")
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	// RunMessage is nil — executeJob should return early without panic.
	scheduler.executeJob(job) // Should not panic.
}

// --- 7. Start / Stop ---

func TestStartAndStop(t *testing.T) {
	scheduler := newTestScheduler(t)

	if err := scheduler.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	// Stop should not panic or block indefinitely.
	scheduler.Stop()
}

// --- 8. Multiple tick execution ---

func TestTick_MultipleJobs(t *testing.T) {
	scheduler := newTestScheduler(t)

	// Two jobs, both matching "* * * * *".
	for _, name := range []string{"alpha", "beta"} {
		job := sampleJob("job-"+name, name)
		job.Schedule = "* * * * *"
		if err := scheduler.CreateAndReload(job); err != nil {
			t.Fatalf("CreateAndReload(%s) error: %v", name, err)
		}
	}

	var mutex sync.Mutex
	executedMessages := map[string]bool{}
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedMessages[message] = true
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	scheduler.tick(time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC))
	time.Sleep(300 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if len(executedMessages) != 2 {
		t.Errorf("expected 2 executed jobs, got %d", len(executedMessages))
	}
}

// --- 9. Tick interval and cron deduplication ---

func TestTickInterval(t *testing.T) {
	if tickInterval != 5*time.Second {
		t.Errorf("tickInterval = %v, want 5s", tickInterval)
	}
}

func TestTick_CronDeduplication_SameMinute(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-dedup", "Dedup")
	job.Schedule = "* * * * *"
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	executedCount := 0
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedCount++
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	// Simulate multiple ticks within the same minute (as the 5-second ticker would).
	base := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	scheduler.tick(base)
	scheduler.tick(base.Add(5 * time.Second))
	scheduler.tick(base.Add(10 * time.Second))
	time.Sleep(200 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if executedCount != 1 {
		t.Errorf("expected cron job to fire once per minute, got %d executions", executedCount)
	}
}

func TestTick_CronDeduplication_DifferentMinutes(t *testing.T) {
	scheduler := newTestScheduler(t)

	job := sampleJob("job-dedup2", "Dedup2")
	job.Schedule = "* * * * *"
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	executedCount := 0
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedCount++
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	// Tick in two different minutes — should fire twice.
	scheduler.tick(time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC))
	scheduler.tick(time.Date(2025, 6, 15, 10, 31, 0, 0, time.UTC))
	time.Sleep(200 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if executedCount != 2 {
		t.Errorf("expected cron job to fire once per minute across 2 minutes, got %d", executedCount)
	}
}

func TestTick_OneShotRunAt_NotDeduplicatedByCronLogic(t *testing.T) {
	scheduler := newTestScheduler(t)

	tickTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	job := sampleJob("job-oneshot-nodup", "OneShotNoDup")
	job.Schedule = ""
	job.RunAt = tickTime.Add(-time.Minute).UnixMilli()
	job.OneShot = true
	job.Enabled = true
	if err := scheduler.CreateAndReload(job); err != nil {
		t.Fatalf("CreateAndReload error: %v", err)
	}

	var mutex sync.Mutex
	executedCount := 0
	scheduler.RunMessage = func(ctx context.Context, agentId, conversationId, message, model string) (string, <-chan struct{}, func() error) {
		mutex.Lock()
		executedCount++
		mutex.Unlock()
		channel := make(chan struct{})
		close(channel)
		return "run-1", channel, func() error { return nil }
	}

	// One-shot jobs use RunAt, not cron — they go through the RunAt path, not cron dedup.
	scheduler.tick(tickTime)
	time.Sleep(300 * time.Millisecond)

	mutex.Lock()
	defer mutex.Unlock()
	if executedCount != 1 {
		t.Errorf("expected one-shot job to fire, got %d executions", executedCount)
	}
}
