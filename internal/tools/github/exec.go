package github

import (
	"context"

	"github.com/teanode/teanode/internal/tools/cliexec"
)

const maxOutputBytes = cliexec.MaxOutputBytes

// commandRunner abstracts command execution for testing.
type commandRunner = cliexec.Runner

// defaultRunner executes commands via cmdexec.Run with process-group isolation.
var defaultRunner = cliexec.NewRunner("github")

// executorConfig holds the GitHub-specific strings for cliexec.
var executorConfig = cliexec.Config{
	ErrorPrefix:  "github",
	CommandLabel: "gh",
	AuthPhrases: []string{
		"not logged into",
		"authentication required",
		"auth login",
		"try authenticating",
		"token expired",
		"invalid token",
	},
	AuthHelp: "GitHub authentication required. Please run 'gh auth login' to authenticate",
}

// execGitHub runs a gh subcommand with the given arguments.
// Each tool builds its own complete arguments including --json <fields>.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitHub(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	log.Debugf("exec: %s %v", binary, arguments)
	return cliexec.Exec(ctx, executorConfig, runner, binary, arguments...)
}

// isAuthError checks if an error message indicates a GitHub authentication problem.
func isAuthError(message string) bool {
	return cliexec.IsAuthError(message, executorConfig.AuthPhrases)
}
