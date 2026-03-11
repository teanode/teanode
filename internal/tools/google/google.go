// Package google exposes provider tools backed by Google Workspace services.
package google

import (
	"context"
	"os/exec"

	"github.com/op/go-logging"

	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("google")

// defaultServices are registered when no explicit service list is configured.
var defaultServices = []string{"gmail", "calendar", "drive"}

type resolvedConfiguration struct {
	binaryPath string
	account    string
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
		if storedConfiguration.Tools != nil && storedConfiguration.Tools.Google != nil {
			googleConfiguration := storedConfiguration.Tools.Google
			configuration.binaryPath = googleConfiguration.GetBinaryPath()
			configuration.account = googleConfiguration.GetAccount()
			configuration.services = googleConfiguration.GetServices()
		}
		return nil
	})
	return configuration
}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "gog"

	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("Google tools skipped: %s binary not found", binary)
		return nil
	}
	log.Infof("Google tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner
	var result []tools.Tool

	for _, service := range defaultServices {
		switch service {
		case "gmail":
			result = append(result, &gmailTool{binary: binary, runner: runner})
		case "calendar":
			result = append(result, &calendarTool{binary: binary, runner: runner})
		case "drive":
			result = append(result, &driveTool{binary: binary, runner: runner})
		case "contacts":
			result = append(result, &contactsTool{binary: binary, runner: runner})
		case "tasks":
			result = append(result, &tasksTool{binary: binary, runner: runner})
		}
	}
	return result
}
