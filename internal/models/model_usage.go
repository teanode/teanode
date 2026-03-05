package models

import "time"

// IntervalType represents the rollup bucket granularity.
type IntervalType string

const (
	IntervalHourly IntervalType = "hourly"
	IntervalDaily  IntervalType = "daily"
)

// ModelUsageEvent represents a single LLM provider request captured at
// the instrumentation point in runner.go.
type ModelUsageEvent struct {
	ID                  string    `json:"id"`
	UserID              string    `json:"userId"`
	ConversationID      string    `json:"conversationId"`
	MessageID           string    `json:"messageId"`
	RunID               string    `json:"runId"`
	ProviderName        string    `json:"providerName"`
	ModelName           string    `json:"modelName"`
	PromptTokens        uint64    `json:"promptTokens"`
	CompletionTokens    uint64    `json:"completionTokens"`
	CacheCreationTokens uint64    `json:"cacheCreationTokens"`
	CacheReadTokens     uint64    `json:"cacheReadTokens"`
	TotalTokens         uint64    `json:"totalTokens"`
	CreatedAt           time.Time `json:"createdAt"`
}

// ModelUsageStatEntry represents a pre-aggregated usage bucket for a
// specific provider/model combination at a given interval granularity.
type ModelUsageStatEntry struct {
	UserID              string       `json:"userId"`
	ProviderName        string       `json:"providerName"`
	ModelName           string       `json:"modelName"`
	IntervalType        IntervalType `json:"intervalType"`
	StartedAt           time.Time    `json:"startedAt"`
	PromptTokens        uint64       `json:"promptTokens"`
	CompletionTokens    uint64       `json:"completionTokens"`
	CacheCreationTokens uint64       `json:"cacheCreationTokens"`
	CacheReadTokens     uint64       `json:"cacheReadTokens"`
	TotalTokens         uint64       `json:"totalTokens"`
	RequestCount        uint64       `json:"requestCount"`
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
