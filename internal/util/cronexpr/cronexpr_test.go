package cronexpr

import (
	"testing"
	"time"
)

func TestParseAndMatch(test *testing.T) {
	cases := []struct {
		expression string
		timestamp  string // RFC3339
		match      bool
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

	for _, testCase := range cases {
		test.Run(testCase.expression+"@"+testCase.timestamp, func(test *testing.T) {
			expression, err := Parse(testCase.expression)
			if err != nil {
				test.Fatalf("Parse(%q) error: %v", testCase.expression, err)
			}
			when, err := time.Parse(time.RFC3339, testCase.timestamp)
			if err != nil {
				test.Fatalf("bad test time: %v", err)
			}
			got := expression.Matches(when)
			if got != testCase.match {
				test.Errorf("Parse(%q).Matches(%s) = %v, want %v", testCase.expression, testCase.timestamp, got, testCase.match)
			}
		})
	}
}

func TestParseErrors(test *testing.T) {
	invalid := []string{
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
	for _, expression := range invalid {
		test.Run(expression, func(test *testing.T) {
			_, err := Parse(expression)
			if err == nil {
				test.Errorf("Parse(%q) expected error, got nil", expression)
			}
		})
	}
}
