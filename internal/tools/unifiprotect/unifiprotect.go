package unifiprotect

import (
	"context"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("unifiprotect")

// resolvedConfig holds the resolved UniFi Protect configuration
// read from the store at execution time.
type resolvedConfig struct {
	baseURL               string
	apiKey                string
	username              string
	password              string
	verifyTLS             bool
	readOnly              bool
	allowedCameras        []string
	allowDangerousActions []string
	timeoutSeconds        int
}

// configFromContext reads the UniFi Protect tool configuration from the store.
func configFromContext(ctx context.Context) *resolvedConfig {
	config := &resolvedConfig{}
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return config
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if configuration.Tools != nil && configuration.Tools.UniFiProtect != nil {
			upConfig := configuration.Tools.UniFiProtect
			config.baseURL = upConfig.GetBaseURL()
			config.apiKey = upConfig.GetAPIKey()
			config.username = upConfig.GetUsername()
			config.password = upConfig.GetPassword()
			config.verifyTLS = upConfig.GetVerifyTLS()
			config.readOnly = upConfig.GetReadOnly()
			config.allowedCameras = upConfig.GetAllowedCameras()
			config.allowDangerousActions = upConfig.GetAllowDangerousActions()
			config.timeoutSeconds = upConfig.GetTimeoutSeconds()
		}
		return nil
	})
	return config
}

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&unifiProtectTool{}}
	})
}
