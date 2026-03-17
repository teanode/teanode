// Package mattermost exposes provider tools backed by the Mattermost CLI (mm).
package mattermost

import (
	"context"
	"os/exec"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("mattermost")

// defaultServices are registered when no explicit service list is configured.
var defaultServices = []string{"channels", "posts", "users"}

type resolvedConfiguration struct {
	binaryPath string
	services   []string
}

func configurationFromContext(ctx context.Context) *resolvedConfiguration {
	configuration := &resolvedConfiguration{}
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return configuration
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		storedConfiguration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if storedConfiguration.Tools != nil && storedConfiguration.Tools.Mattermost != nil {
			mattermostConfiguration := storedConfiguration.Tools.Mattermost
			configuration.binaryPath = mattermostConfiguration.GetBinaryPath()
			configuration.services = mattermostConfiguration.GetServices()
		}
		return nil
	})
	return configuration
}

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
