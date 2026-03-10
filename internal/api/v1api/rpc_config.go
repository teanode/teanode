package v1api

import (
	"context"
	"encoding/json"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/schemas"
	"github.com/teanode/teanode/internal/store"
)

// --- Models RPC handlers ---

// modelsListEntry is a single model in the models.list response.
type modelsListEntry struct {
	ProviderName  string `json:"providerName"`
	ID            string `json:"id"`
	ContextLength int    `json:"context_length,omitempty"`
}

// handleModelsList: return available models from all providers.
func (self *webSocketConnection) handleModelsList(frame requestFrame) (interface{}, error) {
	defaultProviderModelName := ""
	if configuration, err := self.loadConfiguration(); err == nil && configuration != nil {
		if configuration.Models != nil {
			if configuration.Models.Default != nil {
				defaultProviderModelName = *configuration.Models.Default
			}
		}
	}

	var entries []modelsListEntry
	if providerRegistry := self.api.coordinator.ProviderRegistry(); providerRegistry != nil {
		providerModels := providerRegistry.ListAllModels(self.ctx)
		entries = make([]modelsListEntry, len(providerModels))
		for index, entry := range providerModels {
			entries[index] = modelsListEntry{
				ProviderName:  entry.ProviderName,
				ID:            entry.ModelName,
				ContextLength: entry.ContextLength,
			}
		}
	}
	if entries == nil {
		entries = []modelsListEntry{}
	}

	return map[string]interface{}{
		"models":                   entries,
		"defaultProviderModelName": defaultProviderModelName,
	}, nil
}

// --- Config RPC handlers ---

// handleConfigSchema: return the config schema for UI form generation.
func (self *webSocketConnection) handleConfigSchema(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"schema": schemas.ConfigSchema(),
	}, nil
}

// handleConfigGet: return the raw on-disk config.
// Only returns user-specified values, not defaults or environment overrides.
func (self *webSocketConnection) handleConfigGet(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	var configuration *models.Configuration
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		result, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		configuration = result
		return nil
	}); err != nil {
		return nil, rpcError(500, "loading config: "+err.Error())
	}
	return map[string]interface{}{
		"config": configuration,
	}, nil
}

// configUpdateParameters are the parameters for configs.update.
type configUpdateParameters struct {
	Config json.RawMessage `json:"config"`
}

// handleConfigUpdate: merge a partial config into the raw on-disk config and save.
// Only user-specified values are persisted; defaults and env overrides are not saved.
func (self *webSocketConnection) handleConfigUpdate(frame requestFrame) (interface{}, error) {
	if err := self.requireAdmin(); err != nil {
		return nil, err
	}
	parameters, err := unmarshalParams[configUpdateParameters](frame)
	if err != nil {
		return nil, err
	}

	// Parse the incoming partial config into a typed struct.
	var partialConfiguration models.Configuration
	if err := json.Unmarshal(parameters.Config, &partialConfiguration); err != nil {
		return nil, rpcError(400, "invalid config object: "+err.Error())
	}

	// Deep merge via generated Update(): only non-nil fields are applied,
	// nested structs are recursively merged.
	if err := store.StoreFromContext(self.ctx).Transaction(self.ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			configuration.Update(&partialConfiguration)
			return nil
		}, nil)
		return modifyError
	}); err != nil {
		return nil, rpcError(500, "saving config: "+err.Error())
	}

	return map[string]interface{}{
		"ok": true,
	}, nil
}
