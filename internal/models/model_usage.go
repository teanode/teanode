package models

import "time"

// IntervalType represents the rollup bucket granularity.
type IntervalType string

const (
	IntervalHourly IntervalType = "hourly"
	IntervalDaily  IntervalType = "daily"
)

// Usage represents a pre-aggregated usage bucket for a specific
// provider/model combination at a given interval granularity.
type Usage struct {
	UserID              string       `json:"userId" msgpack:"userId"`
	ProviderName        string       `json:"providerName" msgpack:"providerName"`
	ModelName           string       `json:"modelName" msgpack:"modelName"`
	IntervalType        IntervalType `json:"intervalType" msgpack:"intervalType"`
	StartedAt           time.Time    `json:"startedAt" msgpack:"startedAt"`
	PromptTokens        uint64       `json:"promptTokens" msgpack:"promptTokens"`
	CompletionTokens    uint64       `json:"completionTokens" msgpack:"completionTokens"`
	CacheCreationTokens uint64       `json:"cacheCreationTokens" msgpack:"cacheCreationTokens"`
	CacheReadTokens     uint64       `json:"cacheReadTokens" msgpack:"cacheReadTokens"`
	TotalTokens         uint64       `json:"totalTokens" msgpack:"totalTokens"`
	RequestCount        uint64       `json:"requestCount" msgpack:"requestCount"`
}

// UsageKey uniquely identifies a usage bucket.
// StartedAtUnix uses Unix seconds to ensure safe comparison after serialization.
type UsageKey struct {
	UserID        string
	ProviderName  string
	ModelName     string
	IntervalType  IntervalType
	StartedAtUnix int64
}

// Key returns the UsageKey for this Usage entry.
func (u *Usage) Key() UsageKey {
	return UsageKey{
		UserID:        u.UserID,
		ProviderName:  u.ProviderName,
		ModelName:     u.ModelName,
		IntervalType:  u.IntervalType,
		StartedAtUnix: u.StartedAt.Unix(),
	}
}

// BucketStartedAt truncates a time.Time to the given interval boundary
// in the specified local timezone. Returns a timezone-naive local wall-clock
// time (carried in time.UTC) suitable for storing in TIMESTAMP WITHOUT TIME ZONE.
func BucketStartedAt(t time.Time, interval IntervalType, loc *time.Location) time.Time {
	local := t.In(loc)
	switch interval {
	case IntervalHourly:
		return time.Date(local.Year(), local.Month(), local.Day(),
			local.Hour(), 0, 0, 0, time.UTC)
	case IntervalDaily:
		return time.Date(local.Year(), local.Month(), local.Day(),
			0, 0, 0, 0, time.UTC)
	default:
		panic("unknown interval type: " + string(interval))
	}
}
