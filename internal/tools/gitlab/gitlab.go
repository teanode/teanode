package gitlab

import (
	"os/exec"

	"github.com/op/go-logging"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("gitlab")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"issues", "merge_requests", "projects"}

// RegisterTools adds GitLab tools to the registry.
// If the glab binary is not found, no tools are registered.
func RegisterTools(registry *toolregistry.ToolRegistry) {
	binary := "glab"

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("GitLab tools skipped: %s binary not found", binary)
		return
	}
	log.Infof("GitLab tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner

	for _, service := range defaultServices {
		switch service {
		case "issues":
			registry.Register(&issuesTool{binary: resolvedPath, runner: runner})
		case "merge_requests":
			registry.Register(&mergeRequestsTool{binary: resolvedPath, runner: runner})
		case "projects":
			registry.Register(&projectsTool{binary: resolvedPath, runner: runner})
		case "pipelines":
			registry.Register(&pipelinesTool{binary: resolvedPath, runner: runner})
		case "releases":
			registry.Register(&releasesTool{binary: resolvedPath, runner: runner})
		}
	}
}
