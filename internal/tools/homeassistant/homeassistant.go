package homeassistant

import (
	"context"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("homeassistant")

// resolvedConfig holds the resolved Home Assistant configuration
// read from the store at execution time.
type resolvedConfig struct {
	baseURL         string
	token           string
	readOnly        bool
	allowedDomains  []string
	blockedDomains  []string
	allowedEntities []string
	timeoutSeconds  int
}

// configFromContext reads the Home Assistant tool configuration from the store.
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
		if configuration.Tools != nil && configuration.Tools.HomeAssistant != nil {
			haConfig := configuration.Tools.HomeAssistant
			config.baseURL = haConfig.GetBaseURL()
			config.token = haConfig.GetToken()
			config.readOnly = haConfig.GetReadOnly()
			config.allowedDomains = haConfig.GetAllowedDomains()
			config.blockedDomains = haConfig.GetBlockedDomains()
			config.allowedEntities = haConfig.GetAllowedEntities()
			config.timeoutSeconds = haConfig.GetTimeoutSeconds()
		}
		return nil
	})
	return config
}

// RegisterTools adds the Home Assistant tool to the registry.
func RegisterTools(registry *toolregistry.ToolRegistry) {
	registry.Register(&homeAssistantTool{})
}
