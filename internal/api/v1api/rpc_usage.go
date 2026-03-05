package v1api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

// --- usage.statEntries ---

func (self *webSocketConnection) handleUsageStatEntries(frame requestFrame) {
	var parameters struct {
		IntervalType string  `json:"intervalType"`
		StartedAt    string  `json:"startedAt"`
		EndedAt      string  `json:"endedAt"`
		ProviderName *string `json:"providerName"`
		ModelName    *string `json:"modelName"`
		UserID       *string `json:"userId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}

	intervalType := models.IntervalType(parameters.IntervalType)
	if intervalType != models.IntervalHourly && intervalType != models.IntervalDaily {
		self.sendError(frame.ID, 400, "intervalType must be 'hourly' or 'daily'")
		return
	}

	startedAt, err := time.Parse("2006-01-02T15:04:05", parameters.StartedAt)
	if err != nil {
		self.sendError(frame.ID, 400, "startedAt must be in format 2006-01-02T15:04:05")
		return
	}
	endedAt, err := time.Parse("2006-01-02T15:04:05", parameters.EndedAt)
	if err != nil {
		self.sendError(frame.ID, 400, "endedAt must be in format 2006-01-02T15:04:05")
		return
	}

	queryUserID := self.userId()
	if parameters.UserID != nil && *parameters.UserID != queryUserID {
		if !self.isAdmin() {
			self.sendError(frame.ID, 403, "only admins can query other users' usage")
			return
		}
		queryUserID = *parameters.UserID
	}

	query := store.ModelUsageStatQuery{
		UserID:       queryUserID,
		IntervalType: intervalType,
		StartedAt:    startedAt,
		EndedAt:      endedAt,
		ProviderName: parameters.ProviderName,
		ModelName:    parameters.ModelName,
	}

	var entries []*models.ModelUsageStatEntry
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, tx store.Transaction) error {
		result, err := tx.QueryModelUsageStatEntries(ctx, query, nil)
		if err != nil {
			return err
		}
		entries = result
		return nil
	}); err != nil {
		self.sendError(frame.ID, 500, "querying usage: "+err.Error())
		return
	}

	self.sendResponse(frame.ID, map[string]interface{}{
		"entries": entries,
	})
}
