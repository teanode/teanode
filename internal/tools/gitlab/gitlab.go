package gitlab

import (
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("gitlab")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"issues", "merge_requests", "projects"}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "glab"

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("GitLab tools skipped: %s binary not found", binary)
		return nil
	}
	log.Infof("GitLab tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner
	var result []tools.Tool

	for _, service := range defaultServices {
		switch service {
		case "issues":
			result = append(result, &issuesTool{binary: binary, runner: runner})
		case "merge_requests":
			result = append(result, &mergeRequestsTool{binary: binary, runner: runner})
		case "projects":
			result = append(result, &projectsTool{binary: binary, runner: runner})
		case "pipelines":
			result = append(result, &pipelinesTool{binary: binary, runner: runner})
		case "releases":
			result = append(result, &releasesTool{binary: binary, runner: runner})
		}
	}
	return result
}
