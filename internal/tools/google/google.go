package google

import (
	"context"
	"os/exec"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/store"
	toolregistry "github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("google")

// defaultServices are registered when no explicit service list is configured (Tier 1).
var defaultServices = []string{"gmail", "calendar", "drive"}

// resolvedConfig holds the resolved Google tool configuration.
type resolvedConfig struct {
	binaryPath string
	account    string
	services   []string
}

// configFromContext reads the Google tool configuration from the store.
func configFromContext(ctx context.Context) *resolvedConfig {
	var config resolvedConfig
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return &config
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if configuration.Tools != nil && configuration.Tools.Google != nil {
			config.binaryPath = configuration.Tools.Google.GetBinaryPath()
			config.account = configuration.Tools.Google.GetAccount()
			config.services = configuration.Tools.Google.GetServices()
		}
		return nil
	})
	return &config
}

// RegisterTools adds Google Workspace tools to the registry.
func RegisterTools(registry *toolregistry.ToolRegistry) {
	binary := "gog"

	// Check that the binary exists on PATH.
	resolvedPath, err := exec.LookPath(binary)
	if err != nil {
		log.Infof("Google tools skipped: %s binary not found", binary)
		return
	}
	log.Infof("Google tools enabled (binary: %s)", resolvedPath)

	runner := defaultRunner

	for _, service := range defaultServices {
		switch service {
		case "gmail":
			registry.Register(&gmailTool{binary: resolvedPath, runner: runner})
		case "calendar":
			registry.Register(&calendarTool{binary: resolvedPath, runner: runner})
		case "tasks":
			registry.Register(&tasksTool{binary: resolvedPath, runner: runner})
		case "drive":
			registry.Register(&driveTool{binary: resolvedPath, runner: runner})
		case "contacts":
			registry.Register(&contactsTool{binary: resolvedPath, runner: runner})
		}
	}
}
