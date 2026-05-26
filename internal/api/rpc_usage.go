package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/timeutil"
)

// --- usages.list ---

func (self *webSocketConnection) handleListUsages(frame requestFrame) (interface{}, error) {
	var parameters struct {
		IntervalType string  `json:"intervalType"`
		StartedAt    string  `json:"startedAt"`
		EndedAt      string  `json:"endedAt"`
		ProviderName *string `json:"providerName"`
		ModelName    *string `json:"modelName"`
		UserID       *string `json:"userId"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}

	intervalType := timeutil.IntervalType(parameters.IntervalType)
	switch intervalType {
	case timeutil.IntervalTypeHour, timeutil.IntervalTypeDay, timeutil.IntervalTypeWeek, timeutil.IntervalTypeMonth, timeutil.IntervalTypeYear:
	default:
		return nil, rpcError(400, "intervalType must be 'hour', 'day', 'week', 'month', or 'year'")
	}

	startedAt, err := time.ParseInLocation("2006-01-02T15:04:05", parameters.StartedAt, timeutil.LocalLocation())
	if err != nil {
		return nil, rpcError(400, "startedAt must be in format 2006-01-02T15:04:05")
	}
	endedAt, err := time.ParseInLocation("2006-01-02T15:04:05", parameters.EndedAt, timeutil.LocalLocation())
	if err != nil {
		return nil, rpcError(400, "endedAt must be in format 2006-01-02T15:04:05")
	}

	// Non-admin users can only query their own usage.
	// Admins can query a specific user (by passing userId) or all users (by omitting userId).
	var filterUserId *string
	if self.isAdmin() {
		filterUserId = parameters.UserID
	} else {
		if parameters.UserID != nil && *parameters.UserID != self.userId() {
			return nil, rpcError(403, "only admins can query other users' usage")
		}
		currentUserId := self.userId()
		filterUserId = &currentUserId
	}

	listOptions := store.UsageListOptions{
		UserID:       filterUserId,
		IntervalType: intervalType,
		StartedAt:    startedAt,
		EndedAt:      endedAt,
		ProviderName: parameters.ProviderName,
		ModelName:    parameters.ModelName,
	}

	var entries []*models.Usage
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.ListUsages(ctx, listOptions, nil)
		if err != nil {
			return err
		}
		entries = result
		return nil
	}); err != nil {
		return nil, rpcError(500, "querying usage: "+err.Error())
	}

	return map[string]interface{}{
		"entries": entries,
	}, nil
}
