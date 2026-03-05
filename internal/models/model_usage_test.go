package models

import (
	"testing"
	"time"
)

func TestBucketStartedAtHourly(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "truncate to hour",
			input:    time.Date(2026, 3, 5, 19, 37, 42, 0, loc), // 7:37 PM ET
			expected: time.Date(2026, 3, 5, 19, 0, 0, 0, time.UTC),
		},
		{
			name:     "already at hour boundary",
			input:    time.Date(2026, 3, 5, 15, 0, 0, 0, loc),
			expected: time.Date(2026, 3, 5, 15, 0, 0, 0, time.UTC),
		},
		{
			name:     "midnight",
			input:    time.Date(2026, 3, 5, 0, 0, 0, 0, loc),
			expected: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "end of day",
			input:    time.Date(2026, 3, 5, 23, 59, 59, 999, loc),
			expected: time.Date(2026, 3, 5, 23, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BucketStartedAt(tt.input, IntervalHourly, loc)
			if !result.Equal(tt.expected) {
				t.Errorf("BucketStartedAt(%v, hourly) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBucketStartedAtDaily(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "truncate to day",
			input:    time.Date(2026, 3, 5, 19, 37, 42, 0, loc),
			expected: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "midnight stays same",
			input:    time.Date(2026, 3, 5, 0, 0, 0, 0, loc),
			expected: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BucketStartedAt(tt.input, IntervalDaily, loc)
			if !result.Equal(tt.expected) {
				t.Errorf("BucketStartedAt(%v, daily) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBucketStartedAtDSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	// 2026-11-01: DST fall-back in America/New_York at 2:00 AM → 1:00 AM.
	// Both 1:30 AM EDT and 1:30 AM EST should map to the same hourly bucket.
	// Before fall-back (EDT, UTC-4): 1:30 AM local = 5:30 AM UTC
	beforeFallBack := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	// After fall-back (EST, UTC-5): 1:30 AM local = 6:30 AM UTC
	afterFallBack := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)

	resultBefore := BucketStartedAt(beforeFallBack, IntervalHourly, loc)
	resultAfter := BucketStartedAt(afterFallBack, IntervalHourly, loc)

	// Both should produce 01:00 local naive time.
	expected := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	if !resultBefore.Equal(expected) {
		t.Errorf("before fall-back: got %v, want %v", resultBefore, expected)
	}
	if !resultAfter.Equal(expected) {
		t.Errorf("after fall-back: got %v, want %v", resultAfter, expected)
	}
	if !resultBefore.Equal(resultAfter) {
		t.Error("DST fall-back: both occurrences of 1:00 AM should map to the same bucket")
	}
}

func TestBucketStartedAtDSTSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	// 2026-03-08: DST spring-forward in America/New_York at 2:00 AM → 3:00 AM.
	// An event at 2:30 AM UTC (which is 9:30 PM EST the previous day, wait no...)
	// Let's use a UTC time that maps to 3:30 AM EDT (after spring-forward).
	// 3:30 AM EDT = 7:30 AM UTC
	afterSpringForward := time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC)
	result := BucketStartedAt(afterSpringForward, IntervalHourly, loc)
	expected := time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("spring-forward: got %v, want %v", result, expected)
	}

	// An event at 1:30 AM EST (before spring-forward, 6:30 AM UTC)
	beforeSpringForward := time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC)
	result = BucketStartedAt(beforeSpringForward, IntervalHourly, loc)
	expected = time.Date(2026, 3, 8, 1, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("before spring-forward: got %v, want %v", result, expected)
	}

	// Daily bucket: both should land on same day.
	dayBefore := BucketStartedAt(beforeSpringForward, IntervalDaily, loc)
	dayAfter := BucketStartedAt(afterSpringForward, IntervalDaily, loc)
	expectedDay := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)
	if !dayBefore.Equal(expectedDay) {
		t.Errorf("daily before spring-forward: got %v, want %v", dayBefore, expectedDay)
	}
	if !dayAfter.Equal(expectedDay) {
		t.Errorf("daily after spring-forward: got %v, want %v", dayAfter, expectedDay)
	}
}

func TestBucketStartedAtUTCInput(t *testing.T) {
	loc := time.UTC
	input := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)

	hourly := BucketStartedAt(input, IntervalHourly, loc)
	if !hourly.Equal(time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("hourly UTC: got %v", hourly)
	}

	daily := BucketStartedAt(input, IntervalDaily, loc)
	if !daily.Equal(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("daily UTC: got %v", daily)
	}
}

func TestBucketStartedAtPanicsOnUnknownInterval(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unknown interval type")
		}
	}()
	BucketStartedAt(time.Now(), IntervalType("weekly"), time.UTC)
}
