package gitlab

import (
	"context"

	"github.com/teanode/teanode/internal/tools/cliexec"
)

const maxOutputBytes = cliexec.MaxOutputBytes

// commandRunner abstracts command execution for testing.
type commandRunner = cliexec.Runner

// defaultRunner executes commands via cmdexec.Run with process-group isolation.
var defaultRunner = cliexec.NewRunner("gitlab")

// executorConfig holds the GitLab-specific strings for cliexec.
var executorConfig = cliexec.Config{
	ErrorPrefix:  "gitlab",
	CommandLabel: "glab",
	AuthPhrases: []string{
		"not logged into",
		"glab auth login",
		"401 Unauthorized",
		"Invalid token",
		"token was revoked",
		"none of the git remotes configured",
	},
	AuthHelp: "GitLab authentication required. Please run 'glab auth login' to authenticate",
}

// execGitLab runs a glab subcommand with the given arguments.
// Each tool builds its own complete arguments including --output json.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitLab(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	log.Debugf("exec: %s %v", binary, arguments)
	return cliexec.Exec(ctx, executorConfig, runner, binary, arguments...)
}

// isAuthError checks if an error message indicates a GitLab authentication problem.
func isAuthError(message string) bool {
	return cliexec.IsAuthError(message, executorConfig.AuthPhrases)
}
