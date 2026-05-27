package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/util/cronexpr"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/timeutil"
)

// TestCronMatchesAfterTimezoneChange proves that when the host timezone changes
// at runtime, cron expressions fire at the correct local time. This was the
// root-cause bug: the scheduler used time.Local (cached at startup) instead of
// re-reading the system timezone, so jobs would fire at the old local hour
// after a timezone change.
func TestCronMatchesAfterTimezoneChange(t *testing.T) {
	// Fixed UTC instant: 2026-06-15 14:00:00 UTC.
	// In America/New_York (UTC-4 in June) → 10:00 local.
	// In Asia/Tokyo      (UTC+9)          → 23:00 local.
	utcInstant := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)

	// A cron expression that fires at 10:00 every day.
	cronAt10, err := cronexpr.Parse("0 10 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// A cron expression that fires at 23:00 every day.
	cronAt23, err := cronexpr.Parse("0 23 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// --- Phase 1: TZ = America/New_York ---
	t.Setenv("TZ", "America/New_York")
	timeutil.InvalidateLocationCache()
	localTime := utcInstant.In(timeutil.LocalLocation())

	if !cronAt10.Matches(localTime) {
		t.Errorf("with TZ=America/New_York, cron '0 10 * * *' should match local hour %d", localTime.Hour())
	}
	if cronAt23.Matches(localTime) {
		t.Errorf("with TZ=America/New_York, cron '0 23 * * *' should NOT match local hour %d", localTime.Hour())
	}

	// --- Phase 2: change TZ to Asia/Tokyo ---
	t.Setenv("TZ", "Asia/Tokyo")
	timeutil.InvalidateLocationCache()
	localTime = utcInstant.In(timeutil.LocalLocation())

	if cronAt23.Matches(localTime) != true {
		t.Errorf("with TZ=Asia/Tokyo, cron '0 23 * * *' should match local hour %d", localTime.Hour())
	}
	if cronAt10.Matches(localTime) {
		t.Errorf("with TZ=Asia/Tokyo, cron '0 10 * * *' should NOT match local hour %d", localTime.Hour())
	}
}

// TestTickConvertsBehavior verifies the scheduler's tick method converts the
// ticker time to the current local timezone. Without the fix, tick would pass
// the time through with its original (stale) location.
func TestTickConvertsBehavior(t *testing.T) {
	// This test exercises the conversion path in tick(). We can't fully run
	// tick without a store, but we replicate the exact conversion logic and
	// cron matching that tick performs.

	utcInstant := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)

	// Cron: fire at minute 30 of hour 10. Matches New York (10:30 local).
	expression, err := cronexpr.Parse("30 10 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	t.Setenv("TZ", "America/New_York")
	timeutil.InvalidateLocationCache()
	converted := utcInstant.In(timeutil.LocalLocation())
	if !expression.Matches(converted) {
		t.Errorf("expected match at NY local time %s", converted.Format("15:04"))
	}

	// After switching to UTC, the same instant is 14:30 — should NOT match.
	t.Setenv("TZ", "UTC")
	timeutil.InvalidateLocationCache()
	converted = utcInstant.In(timeutil.LocalLocation())
	if expression.Matches(converted) {
		t.Errorf("should not match at UTC time %s", converted.Format("15:04"))
	}
}

func TestTriggerJobWithMetadataCreatesJobRunHistory(t *testing.T) {
	openedStore, openError := fsstore.Open(fsstore.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("Open: %v", openError)
	}
	defer func() { _ = openedStore.Close() }()

	ctx := store.ContextWithStore(context.Background(), openedStore)
	scheduler := NewScheduler(ctx)
	scheduler.RunMessage = func(ctx context.Context, userID, agentID, conversationID, message, providerModelName string) (string, <-chan struct{}, func() error) {
		done := make(chan struct{})
		close(done)
		return "run-1", done, func() error { return nil }
	}

	jobModel := &models.Job{
		UserID:         ptrto.Value("user-1"),
		AgentID:        ptrto.Value("agent-1"),
		ConversationID: ptrto.Value("conversation-1"),
		Name:           ptrto.Value("Webhook"),
		Trigger:        ptrto.Value(models.JobTriggerKindWebhook),
		WebhookSecret:  ptrto.Value("secret"),
		Prompt:         ptrto.Value("Run"),
		Enabled:        ptrto.Value(true),
	}
	if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		createdJob, createError := transaction.CreateJob(ctx, jobModel, nil)
		if createError == nil && createdJob != nil {
			jobModel = createdJob
		}
		return createError
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	jobRun, triggerError := scheduler.TriggerJobWithMetadata(ctx, jobModel.ID, TriggerMetadata{
		Trigger:       models.JobTriggerKindWebhook,
		RequestMethod: "POST",
		RequestPath:   "/api/jobs/" + jobModel.ID + "/webhook",
		RemoteAddress: "127.0.0.1",
	})
	if triggerError != nil {
		t.Fatalf("TriggerJobWithMetadata: %v", triggerError)
	}
	if jobRun == nil || jobRun.ID == "" {
		t.Fatalf("expected created job run")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var listedJobRuns []*models.JobRun
		if err := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
			var listError error
			listedJobRuns, listError = transaction.ListJobRuns(ctx, jobModel.ID, nil)
			return listError
		}); err != nil {
			t.Fatalf("ListJobRuns: %v", err)
		}
		if len(listedJobRuns) == 1 && listedJobRuns[0].GetStatus() == models.JobRunStatusSuccess && listedJobRuns[0].GetRunID() == "run-1" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("job run was not completed successfully before timeout")
}
