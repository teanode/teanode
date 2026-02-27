package google

import (
	"context"
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("google")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"gmail", "calendar", "drive"}

// resolvedConfiguration holds the resolved Google tool configuration.
type resolvedConfiguration struct {
	binaryPath string
	account    string
	services   []string
}

// configurationFromContext reads the Google tool configuration from the store.
func configurationFromContext(ctx context.Context) *resolvedConfiguration {
	var configuration resolvedConfiguration
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return &configuration
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		storedConfiguration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if storedConfiguration.Tools != nil && storedConfiguration.Tools.Google != nil {
			configuration.binaryPath = storedConfiguration.Tools.Google.GetBinaryPath()
			configuration.account = storedConfiguration.Tools.Google.GetAccount()
			configuration.services = storedConfiguration.Tools.Google.GetServices()
		}
		return nil
	})
	return &configuration
}

func init() {
	tools.RegisterBuiltinTool(createTools)
}

func createTools() []tools.Tool {
	binary := "gog"

	// Check that the binary exists on PATH.
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
			result = append(result, &gmailTool{binary: resolvedPath, runner: runner})
		case "calendar":
			result = append(result, &calendarTool{binary: resolvedPath, runner: runner})
		case "tasks":
			result = append(result, &tasksTool{binary: resolvedPath, runner: runner})
		case "drive":
			result = append(result, &driveTool{binary: resolvedPath, runner: runner})
		case "contacts":
			result = append(result, &contactsTool{binary: resolvedPath, runner: runner})
		}
	}
	return result
}
