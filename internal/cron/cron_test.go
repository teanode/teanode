package cron

import (
	"testing"
	"time"
)

func TestParseAndMatch(t *testing.T) {
	tests := []struct {
		expr    string
		time    string // RFC3339
		match   bool
	}{
		// Every 5 minutes
		{"*/5 * * * *", "2025-01-15T10:00:00Z", true},
		{"*/5 * * * *", "2025-01-15T10:05:00Z", true},
		{"*/5 * * * *", "2025-01-15T10:03:00Z", false},
		{"*/5 * * * *", "2025-01-15T10:15:00Z", true},

		// 9am weekdays (Mon-Fri)
		{"0 9 * * 1-5", "2025-01-13T09:00:00Z", true},  // Monday
		{"0 9 * * 1-5", "2025-01-14T09:00:00Z", true},  // Tuesday
		{"0 9 * * 1-5", "2025-01-12T09:00:00Z", false}, // Sunday
		{"0 9 * * 1-5", "2025-01-13T10:00:00Z", false}, // Monday 10am

		// 2:30am on 1st of month
		{"30 2 1 * *", "2025-01-01T02:30:00Z", true},
		{"30 2 1 * *", "2025-02-01T02:30:00Z", true},
		{"30 2 1 * *", "2025-01-02T02:30:00Z", false},
		{"30 2 1 * *", "2025-01-01T03:30:00Z", false},

		// Every minute
		{"* * * * *", "2025-06-15T12:34:00Z", true},
		{"* * * * *", "2025-01-01T00:00:00Z", true},

		// Specific minute/hour
		{"15 14 * * *", "2025-03-10T14:15:00Z", true},
		{"15 14 * * *", "2025-03-10T14:16:00Z", false},

		// Lists
		{"0,30 * * * *", "2025-01-15T10:00:00Z", true},
		{"0,30 * * * *", "2025-01-15T10:30:00Z", true},
		{"0,30 * * * *", "2025-01-15T10:15:00Z", false},

		// Specific months
		{"0 0 1 1,6 *", "2025-01-01T00:00:00Z", true},
		{"0 0 1 1,6 *", "2025-06-01T00:00:00Z", true},
		{"0 0 1 1,6 *", "2025-03-01T00:00:00Z", false},

		// Sunday only
		{"0 12 * * 0", "2025-01-12T12:00:00Z", true},  // Sunday
		{"0 12 * * 0", "2025-01-13T12:00:00Z", false}, // Monday
	}

	for _, tt := range tests {
		t.Run(tt.expr+"@"+tt.time, func(t *testing.T) {
			expr, err := Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.expr, err)
			}
			when, err := time.Parse(time.RFC3339, tt.time)
			if err != nil {
				t.Fatalf("bad test time: %v", err)
			}
			got := expr.Matches(when)
			if got != tt.match {
				t.Errorf("Parse(%q).Matches(%s) = %v, want %v", tt.expr, tt.time, got, tt.match)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",
		"* * *",
		"* * * * * *",
		"60 * * * *",
		"* 25 * * *",
		"* * 0 * *",
		"* * 32 * *",
		"* * * 0 *",
		"* * * 13 *",
		"* * * * 7",
		"*/0 * * * *",
		"abc * * * *",
		"5-3 * * * *",
	}
	for _, expr := range bad {
		t.Run(expr, func(t *testing.T) {
			_, err := Parse(expr)
			if err == nil {
				t.Errorf("Parse(%q) expected error, got nil", expr)
			}
		})
	}
}
