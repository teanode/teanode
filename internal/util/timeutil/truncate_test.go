package timeutil

import (
	"fmt"
	"testing"
	"time"
)

var (
	truncateTestNow = time.Date(2026, 3, 5, 14, 37, 42, 123456789, time.Local)

	truncateTestDayLightSavingEndedAt = func() time.Time {
		newYork, err := time.LoadLocation("America/New_York")
		if err != nil {
			panic(fmt.Sprintf("timeutil: failed to load location: %s", err))
		}
		// 2024-11-03 06:00 UTC = 2024-11-03 01:00 EST (right after DST fall-back in New York)
		return time.Date(2024, 11, 3, 6, 0, 0, 0, time.UTC).In(newYork)
	}()
)

func TestTruncateToHour(t *testing.T) {
	t.Parallel()

	expectedResult := time.Date(2026, 3, 5, 14, 0, 0, 0, time.Local)
	if result := TruncateToHour(truncateTestNow); !expectedResult.Equal(result) {
		t.Fatalf("expected %s but received %s", expectedResult, result)
	}

	if result := TruncateToHour(truncateTestDayLightSavingEndedAt); !truncateTestDayLightSavingEndedAt.Equal(result) {
		t.Fatalf("expected %s but received %s", truncateTestDayLightSavingEndedAt, result)
	}
}

func TestTruncateToDay(t *testing.T) {
	t.Parallel()

	expectedResult := time.Date(2026, 3, 5, 0, 0, 0, 0, time.Local)
	if result := TruncateToDay(truncateTestNow); !expectedResult.Equal(result) {
		t.Fatalf("expected %s but received %s", expectedResult, result)
	}
}

func TestTruncateToWeek(t *testing.T) {
	t.Parallel()

	// 2026-03-05 is a Thursday, so the preceding Sunday is 2026-03-01
	expectedResult := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	if result := TruncateToWeek(truncateTestNow); !expectedResult.Equal(result) {
		t.Fatalf("expected %s but received %s", expectedResult, result)
	}
}

func TestTruncateToMonth(t *testing.T) {
	t.Parallel()

	expectedResult := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	if result := TruncateToMonth(truncateTestNow); !expectedResult.Equal(result) {
		t.Fatalf("expected %s but received %s", expectedResult, result)
	}
}

func TestTruncateToYear(t *testing.T) {
	t.Parallel()

	expectedResult := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	if result := TruncateToYear(truncateTestNow); !expectedResult.Equal(result) {
		t.Fatalf("expected %s but received %s", expectedResult, result)
	}
}
