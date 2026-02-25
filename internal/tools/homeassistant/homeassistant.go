package homeassistant

import (
	"github.com/op/go-logging"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("homeassistant")

type RegistrationOptions struct {
	BaseURL         string
	Token           string
	ReadOnly        bool
	AllowedDomains  []string
	BlockedDomains  []string
	AllowedEntities []string
	TimeoutSeconds  int
}

// RegisterTools adds the Home Assistant tool to the registry.
// If config is nil or missing BaseURL/Token, no tools are registered.
func RegisterTools(registry *toolregistry.ToolRegistry, options *RegistrationOptions) {
	if options == nil {
		return
	}
	if options.BaseURL == "" {
		log.Infof("Home Assistant tool skipped: baseUrl not configured")
		return
	}
	if options.Token == "" {
		log.Infof("Home Assistant tool skipped: token not configured")
		return
	}

	client := NewHTTPClient(options.BaseURL, options.Token, options.TimeoutSeconds)
	checker := NewAccessChecker(options)

	log.Infof("Home Assistant tool enabled (url: %s, readOnly: %v)", options.BaseURL, options.ReadOnly)

	registry.Register(&homeAssistantTool{
		client:  client,
		checker: checker,
	})
}
