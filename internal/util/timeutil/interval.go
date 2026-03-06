package timeutil

import (
	"time"
)

// IntervalType represents a time interval granularity.
type IntervalType string

func (self IntervalType) String() string {
	return string(self)
}

const (
	IntervalTypeHour  IntervalType = "hour"
	IntervalTypeDay   IntervalType = "day"
	IntervalTypeWeek  IntervalType = "week"
	IntervalTypeMonth IntervalType = "month"
	IntervalTypeYear  IntervalType = "year"
)

// TruncateToInterval truncates a timestamp to the closest interval start time.
func TruncateToInterval(timestamp time.Time, intervalType IntervalType) time.Time {
	switch intervalType {
	case IntervalTypeHour:
		return TruncateToHour(timestamp)
	case IntervalTypeDay:
		return TruncateToDay(timestamp)
	case IntervalTypeWeek:
		return TruncateToWeek(timestamp)
	case IntervalTypeMonth:
		return TruncateToMonth(timestamp)
	case IntervalTypeYear:
		return TruncateToYear(timestamp)
	default:
		return timestamp
	}
}
