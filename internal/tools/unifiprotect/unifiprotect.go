package unifiprotect

import (
	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

var log = logging.MustGetLogger("unifiprotect")

// RegisterTools adds the UniFi Protect tool to the registry.
// If config is nil or missing BaseURL and credentials, no tools are registered.
func RegisterTools(registry *agents.ToolRegistry, config *configs.UniFiProtectConfig) {
	if config == nil {
		return
	}
	if config.BaseURL == "" {
		log.Infof("UniFi Protect tool skipped: baseUrl not configured")
		return
	}
	if config.APIKey == "" && (config.Username == "" || config.Password == "") {
		log.Infof("UniFi Protect tool skipped: credentials not configured (need apiKey or username+password)")
		return
	}

	client := NewHTTPClient(config)
	checker := NewAccessChecker(config)

	log.Infof("UniFi Protect tool enabled (url: %s, readOnly: %v)", config.BaseURL, config.ReadOnly)

	registry.Register(&unifiProtectTool{
		client:  client,
		checker: checker,
	})
}
