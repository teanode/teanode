// Package gitea exposes provider tools backed by the Gitea CLI.
package gitea

import (
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("gitea")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"issues", "pulls", "repos"}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "tea"

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("Gitea tools skipped: %s binary not found", binary)
		return nil
	}
	log.Infof("Gitea tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner
	var result []tools.Tool

	for _, service := range defaultServices {
		switch service {
		case "issues":
			result = append(result, &issuesTool{binary: binary, runner: runner})
		case "pulls":
			result = append(result, &pullsTool{binary: binary, runner: runner})
		case "repos":
			result = append(result, &reposTool{binary: binary, runner: runner})
		case "releases":
			result = append(result, &releasesTool{binary: binary, runner: runner})
		}
	}
	return result
}
