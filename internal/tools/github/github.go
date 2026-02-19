package github

import (
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

var log = logging.MustGetLogger("github")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"issues", "pulls", "repos"}

// RegisterTools adds GitHub tools to the registry.
// If the gh binary is not found, no tools are registered.
// A nil config is treated as "use defaults" — tools are registered
// as long as the binary is present on PATH.
func RegisterTools(registry *agents.ToolRegistry, config *configs.GitHubConfig) {
	binary := "gh"
	var services []string
	if config != nil {
		if config.BinaryPath != "" {
			binary = config.BinaryPath
		}
		services = config.Services
	}

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("GitHub tools skipped: %s binary not found", binary)
		return
	}
	log.Infof("GitHub tools enabled (binary: %s)", resolvedPath)

	if len(services) == 0 {
		services = defaultServices
	}

	runner := defaultRunner

	for _, service := range services {
		switch service {
		case "issues":
			registry.Register(&issuesTool{binary: resolvedPath, runner: runner})
		case "pulls":
			registry.Register(&pullsTool{binary: resolvedPath, runner: runner})
		case "repos":
			registry.Register(&reposTool{binary: resolvedPath, runner: runner})
		case "search":
			registry.Register(&searchTool{binary: resolvedPath, runner: runner})
		case "actions":
			registry.Register(&actionsTool{binary: resolvedPath, runner: runner})
		case "releases":
			registry.Register(&releasesTool{binary: resolvedPath, runner: runner})
		default:
			log.Warningf("unknown GitHub service %q, skipping", service)
		}
	}
}
