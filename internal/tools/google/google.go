package google

import (
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

var log = logging.MustGetLogger("google")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"gmail", "calendar", "drive"}

// RegisterTools adds Google Workspace tools to the registry.
// If the gog binary is not found, no tools are registered.
// A nil config is treated as "use defaults" — tools are registered
// as long as the binary is present on PATH.
func RegisterTools(registry *agents.ToolRegistry, config *configs.GoogleConfig) {
	binary := "gog"
	var account string
	var services []string
	if config != nil {
		if config.BinaryPath != "" {
			binary = config.BinaryPath
		}
		account = config.Account
		services = config.Services
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
