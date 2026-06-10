package gitea

import (
	"context"

	"github.com/teanode/teanode/internal/tools/cliexec"
)

const maxOutputBytes = cliexec.MaxOutputBytes

// commandRunner abstracts command execution for testing.
type commandRunner = cliexec.Runner

// defaultRunner executes commands via cmdexec.Run with process-group isolation.
var defaultRunner = cliexec.NewRunner("gitea")

// executorConfig holds the Gitea-specific strings for cliexec.
var executorConfig = cliexec.Config{
	ErrorPrefix:  "gitea",
	CommandLabel: "tea",
	AuthPhrases: []string{
		"No login configured",
		"tea login add",
		"401 Unauthorized",
		"token is required",
		"unauthorized",
	},
	AuthHelp: "gitea authentication required: please run 'tea login add' to authenticate",
}

// execGitea runs a tea subcommand with the given arguments.
// Each tool builds its own complete arguments including --output json.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitea(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	log.Debugf("exec: %s %v", binary, arguments)
	return cliexec.Exec(ctx, executorConfig, runner, binary, arguments...)
}

// isAuthError checks if an error message indicates a Gitea authentication problem.
func isAuthError(message string) bool {
	return cliexec.IsAuthError(message, executorConfig.AuthPhrases)
}
