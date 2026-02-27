package homeassistant

import (
	"context"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("homeassistant")

// resolvedConfiguration holds the resolved Home Assistant configuration
// read from the store at execution time.
type resolvedConfiguration struct {
	baseUrl         string
	token           string
	readOnly        bool
	allowedDomains  []string
	blockedDomains  []string
	allowedEntities []string
	timeoutSeconds  int
}

// configurationFromContext reads the Home Assistant tool configuration from the store.
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
		if storedConfiguration.Tools != nil && storedConfiguration.Tools.HomeAssistant != nil {
			homeAssistantConfiguration := storedConfiguration.Tools.HomeAssistant
			configuration.baseUrl = homeAssistantConfiguration.GetBaseURL()
			configuration.token = homeAssistantConfiguration.GetToken()
			configuration.readOnly = homeAssistantConfiguration.GetReadOnly()
			configuration.allowedDomains = homeAssistantConfiguration.GetAllowedDomains()
			configuration.blockedDomains = homeAssistantConfiguration.GetBlockedDomains()
			configuration.allowedEntities = homeAssistantConfiguration.GetAllowedEntities()
			configuration.timeoutSeconds = homeAssistantConfiguration.GetTimeoutSeconds()
		}
		return nil
	})
	return configuration
}

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&homeAssistantTool{}}
	})
}
