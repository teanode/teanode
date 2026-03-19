// Package mattermost exposes provider tools backed by the Mattermost CLI (mm).
package mattermost

import (
	"os/exec"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("mattermost")

// defaultServices are registered when no explicit service list is configured.
var defaultServices = []string{"channels", "posts", "users"}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "mm"

	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("Mattermost tools skipped: %s binary not found", binary)
		return nil
	}
	log.Infof("Mattermost tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner
	var result []tools.Tool

	for _, service := range defaultServices {
		switch service {
		case "channels":
			result = append(result, &channelsTool{binary: binary, runner: runner})
		case "posts":
			result = append(result, &postsTool{binary: binary, runner: runner})
		case "users":
			result = append(result, &usersTool{binary: binary, runner: runner})
		}
	}
	return result
}
