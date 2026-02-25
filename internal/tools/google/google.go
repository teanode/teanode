package google

import (
	"os/exec"

	"github.com/op/go-logging"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("google")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"gmail", "calendar", "drive"}

type RegistrationOptions struct {
	BinaryPath string
	Account    string
	Services   []string
}

// RegisterTools adds Google Workspace tools to the registry.
// If the gog binary is not found, no tools are registered.
// A nil config is treated as "use defaults" — tools are registered
// as long as the binary is present on PATH.
func RegisterTools(registry *toolregistry.ToolRegistry, options *RegistrationOptions) {
	binary := "gog"
	var account string
	var services []string
	if options != nil {
		if options.BinaryPath != "" {
			binary = options.BinaryPath
		}
		account = options.Account
		services = options.Services
	}

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("Google tools skipped: %s binary not found", binary)
		return
	}
	log.Infof("Google tools enabled (binary: %s)", resolvedPath)

	if len(services) == 0 {
		services = defaultServices
	}

	runner := defaultRunner

	for _, service := range services {
		switch service {
		case "gmail":
			registry.Register(&gmailTool{binary: resolvedPath, account: account, runner: runner})
		case "calendar":
			registry.Register(&calendarTool{binary: resolvedPath, account: account, runner: runner})
		case "tasks":
			registry.Register(&tasksTool{binary: resolvedPath, account: account, runner: runner})
		case "drive":
			registry.Register(&driveTool{binary: resolvedPath, account: account, runner: runner})
		case "contacts":
			registry.Register(&contactsTool{binary: resolvedPath, account: account, runner: runner})
		default:
			log.Warningf("unknown Google service %q, skipping", service)
		}
	}
}
