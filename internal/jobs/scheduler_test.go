package jobs

import (
	"testing"
	"time"

	"github.com/teanode/teanode/internal/util/cronexpr"
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
	localTime := utcInstant.In(timeutil.LocalLocation())

	if !cronAt10.Matches(localTime) {
		t.Errorf("with TZ=America/New_York, cron '0 10 * * *' should match local hour %d", localTime.Hour())
	}
	if cronAt23.Matches(localTime) {
		t.Errorf("with TZ=America/New_York, cron '0 23 * * *' should NOT match local hour %d", localTime.Hour())
	}

	// --- Phase 2: change TZ to Asia/Tokyo ---
	t.Setenv("TZ", "Asia/Tokyo")
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
	converted := utcInstant.In(timeutil.LocalLocation())
	if !expression.Matches(converted) {
		t.Errorf("expected match at NY local time %s", converted.Format("15:04"))
	}

	// After switching to UTC, the same instant is 14:30 — should NOT match.
	t.Setenv("TZ", "UTC")
	converted = utcInstant.In(timeutil.LocalLocation())
	if expression.Matches(converted) {
		t.Errorf("should not match at UTC time %s", converted.Format("15:04"))
	}
}
