// Package cron provides a scheduler that runs cron jobs on a per-minute tick.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/logging"
)

var log = logging.Get("cron")

// CronExpr is a parsed 5-field cron expression (minute, hour, dayOfMonth, month, dayOfWeek).
type CronExpr struct {
	minute     [60]bool
	hour       [24]bool
	dayOfMonth [31]bool // 0-indexed: dayOfMonth[0] = day 1
	month      [12]bool // 0-indexed: month[0] = January
	dayOfWeek  [7]bool  // 0=Sun, 1=Mon, ..., 6=Sat
}

// Parse parses a 5-field cron expression.
// Supports: *, single values, ranges (1-5), steps (*/15), lists (1,3,5).
func Parse(expression string) (*CronExpr, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	cronExpression := &CronExpr{}

	if err := parseField(fields[0], cronExpression.minute[:], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if err := parseField(fields[1], cronExpression.hour[:], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if err := parseField(fields[2], cronExpression.dayOfMonth[:], 1, 31); err != nil {
		return nil, fmt.Errorf("day-of-month: %w", err)
	}
	if err := parseField(fields[3], cronExpression.month[:], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if err := parseField(fields[4], cronExpression.dayOfWeek[:], 0, 6); err != nil {
		return nil, fmt.Errorf("day-of-week: %w", err)
	}

	return cronExpression, nil
}

// Matches returns true if t falls within this cron expression.
func (self *CronExpr) Matches(when time.Time) bool {
	return self.minute[when.Minute()] &&
		self.hour[when.Hour()] &&
		self.dayOfMonth[when.Day()-1] &&
		self.month[when.Month()-1] &&
		self.dayOfWeek[when.Weekday()]
}

func parseField(field string, bits []bool, min, max int) error {
	offset := min // values are stored at index (value - min)

	for _, part := range strings.Split(field, ",") {
		step := 1
		rangeString := part

		if index := strings.Index(part, "/"); index >= 0 {
			var err error
			step, err = strconv.Atoi(part[index+1:])
			if err != nil || step <= 0 {
				return fmt.Errorf("invalid step in %q", part)
			}
			rangeString = part[:index]
		}

		var low, high int
		if rangeString == "*" {
			low, high = min, max
		} else if index := strings.Index(rangeString, "-"); index >= 0 {
			var err error
			low, err = strconv.Atoi(rangeString[:index])
			if err != nil {
				return fmt.Errorf("invalid range in %q", part)
			}
			high, err = strconv.Atoi(rangeString[index+1:])
			if err != nil {
				return fmt.Errorf("invalid range in %q", part)
			}
		} else {
			value, err := strconv.Atoi(rangeString)
			if err != nil {
				return fmt.Errorf("invalid value %q", rangeString)
			}
			low, high = value, value
		}

		if low < min || high > max || low > high {
			return fmt.Errorf("value out of range in %q (allowed %d-%d)", part, min, max)
		}

		for value := low; value <= high; value += step {
			bits[value-offset] = true
		}
	}

	return nil
}
