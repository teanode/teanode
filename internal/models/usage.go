package models

import (
	"time"

	"github.com/teanode/teanode/internal/util/timeutil"
)

// Usage represents a pre-aggregated usage bucket for a specific
// provider/model combination at a given interval granularity.
type Usage struct {
	UserID       *string `json:"userId,omitempty" msgpack:"userId"`
	ProviderName *string `json:"providerName,omitempty" msgpack:"providerName"`
	ModelName    *string `json:"modelName,omitempty" msgpack:"modelName"`

	IntervalType *timeutil.IntervalType `json:"intervalType,omitempty" msgpack:"intervalType"`
	StartedAt    *time.Time             `json:"startedAt,omitempty" msgpack:"startedAt"`

	PromptTokens        *uint64 `json:"promptTokens,omitempty" msgpack:"promptTokens"`
	CompletionTokens    *uint64 `json:"completionTokens,omitempty" msgpack:"completionTokens"`
	CacheCreationTokens *uint64 `json:"cacheCreationTokens,omitempty" msgpack:"cacheCreationTokens"`
	CacheReadTokens     *uint64 `json:"cacheReadTokens,omitempty" msgpack:"cacheReadTokens"`
	TotalTokens         *uint64 `json:"totalTokens,omitempty" msgpack:"totalTokens"`
	RequestCount        *uint64 `json:"requestCount,omitempty" msgpack:"requestCount"`
}
