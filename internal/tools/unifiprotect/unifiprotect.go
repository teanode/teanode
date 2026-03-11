// Package unifiprotect exposes provider tools backed by UniFi Protect.
package unifiprotect

import (
	"context"

	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

// resolvedConfiguration holds the resolved UniFi Protect configuration
// read from the store at execution time.
type resolvedConfiguration struct {
	baseUrl               string
	apiKey                string
	username              string
	password              string
	verifyTls             bool
	readOnly              bool
	allowedCameras        []string
	allowDangerousActions []string
	timeoutSeconds        int
}

// configurationFromContext reads the UniFi Protect tool configuration from the store.
func configurationFromContext(ctx context.Context) *resolvedConfiguration {
	configuration := &resolvedConfiguration{}
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return configuration
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		storedConfiguration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if storedConfiguration.Tools != nil && storedConfiguration.Tools.UniFiProtect != nil {
			unifiProtectConfiguration := storedConfiguration.Tools.UniFiProtect
			configuration.baseUrl = unifiProtectConfiguration.GetBaseURL()
			configuration.apiKey = unifiProtectConfiguration.GetAPIKey()
			configuration.username = unifiProtectConfiguration.GetUsername()
			configuration.password = unifiProtectConfiguration.GetPassword()
			configuration.verifyTls = unifiProtectConfiguration.GetVerifyTLS()
			configuration.readOnly = unifiProtectConfiguration.GetReadOnly()
			configuration.allowedCameras = unifiProtectConfiguration.GetAllowedCameras()
			configuration.allowDangerousActions = unifiProtectConfiguration.GetAllowDangerousActions()
			configuration.timeoutSeconds = unifiProtectConfiguration.GetTimeoutSeconds()
		}
		return nil
	})
	return configuration
}

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&unifiProtectTool{}}
	})
}
