// Package confluence exposes provider tools backed by the Confluence CLI.
package confluence

import (
	"os/exec"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("confluence")

var defaultServices = []string{"pages", "spaces", "comments"}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "confluence"

	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("Confluence tools skipped: %s binary not found", binary)
		return nil
	}
	log.Infof("Confluence tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner
	var result []tools.Tool

	for _, service := range defaultServices {
		switch service {
		case "pages":
			result = append(result, &pagesTool{binary: binary, runner: runner})
		case "spaces":
			result = append(result, &spacesTool{binary: binary, runner: runner})
		case "comments":
			result = append(result, &commentsTool{binary: binary, runner: runner})
		}
	}
	return result
}
