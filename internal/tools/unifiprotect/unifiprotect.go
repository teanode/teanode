package unifiprotect

import (
	"github.com/op/go-logging"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("unifiprotect")

type RegistrationOptions struct {
	BaseURL               string
	APIKey                string
	Username              string
	Password              string
	VerifyTLS             bool
	ReadOnly              bool
	AllowedCameras        []string
	AllowDangerousActions []string
	TimeoutSeconds        int
}

// RegisterTools adds the UniFi Protect tool to the registry.
// If config is nil or missing BaseURL and credentials, no tools are registered.
func RegisterTools(registry *toolregistry.ToolRegistry, options *RegistrationOptions) {
	if options == nil {
		return
	}
	if options.BaseURL == "" {
		log.Infof("UniFi Protect tool skipped: baseUrl not configured")
		return
	}
	if options.APIKey == "" && (options.Username == "" || options.Password == "") {
		log.Infof("UniFi Protect tool skipped: credentials not configured (need apiKey or username+password)")
		return
	}

	client := NewHTTPClient(options)
	checker := NewAccessChecker(options)

	log.Infof("UniFi Protect tool enabled (url: %s, readOnly: %v)", options.BaseURL, options.ReadOnly)

	registry.Register(&unifiProtectTool{
		client:  client,
		checker: checker,
	})
}
