package github

import (
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("github")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"issues", "pulls", "repos"}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "gh"

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("GitHub tools skipped: %s binary not found", binary)
		return nil
	}
	log.Infof("GitHub tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner
	var result []tools.Tool

	for _, service := range defaultServices {
		switch service {
		case "issues":
			result = append(result, &issuesTool{binary: resolvedPath, runner: runner})
		case "pulls":
			result = append(result, &pullsTool{binary: resolvedPath, runner: runner})
		case "repos":
			result = append(result, &reposTool{binary: resolvedPath, runner: runner})
		case "search":
			result = append(result, &searchTool{binary: resolvedPath, runner: runner})
		case "actions":
			result = append(result, &actionsTool{binary: resolvedPath, runner: runner})
		case "releases":
			result = append(result, &releasesTool{binary: resolvedPath, runner: runner})
		}
	}
	return result
}
