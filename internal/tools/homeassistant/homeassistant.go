package homeassistant

import (
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

var log = logging.MustGetLogger("homeassistant")

// RegisterTools adds the Home Assistant tool to the registry.
// If config is nil or missing BaseURL/Token, no tools are registered.
func RegisterTools(registry *agents.ToolRegistry, config *configs.HomeAssistantConfig) {
	if config == nil {
		return
	}
	if config.BaseURL == "" {
		log.Infof("Home Assistant tool skipped: baseUrl not configured")
		return
	}
	if config.Token == "" {
		log.Infof("Home Assistant tool skipped: token not configured")
		return
	}

	client := NewHTTPClient(config.BaseURL, config.Token, config.TimeoutSeconds)
	checker := NewAccessChecker(config)

	log.Infof("Home Assistant tool enabled (url: %s, readOnly: %v)", config.BaseURL, config.ReadOnly)

	registry.Register(&homeAssistantTool{
		client:  client,
		checker: checker,
	})
}
