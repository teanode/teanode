package timeutil

import (
	"time"
)

func TruncateToHour(timestamp time.Time) time.Time {
	duration := time.Duration(timestamp.Nanosecond()) * time.Nanosecond
	duration += time.Duration(timestamp.Second()) * time.Second
	duration += time.Duration(timestamp.Minute()) * time.Minute
	return timestamp.Add(-duration)
}

func TruncateToDay(timestamp time.Time) time.Time {
	return time.Date(timestamp.Year(), timestamp.Month(), timestamp.Day(), 0, 0, 0, 0, timestamp.Location())
}

func TruncateToWeek(timestamp time.Time) time.Time {
	return TruncateToDay(timestamp.AddDate(0, 0, -int(timestamp.Weekday())))
}

func TruncateToMonth(timestamp time.Time) time.Time {
	return time.Date(timestamp.Year(), timestamp.Month(), 1, 0, 0, 0, 0, timestamp.Location())
}

func TruncateToYear(timestamp time.Time) time.Time {
	return time.Date(timestamp.Year(), 1, 1, 0, 0, 0, 0, timestamp.Location())
}
