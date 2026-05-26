package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/util/cmdexec"
)

const (
	maxOutputBytes = 50 * 1024 // 50 KB output truncation limit
	execTimeout    = 60 * time.Second
)

// commandRunner abstracts command execution for testing.
type commandRunner func(ctx context.Context, name string, arguments ...string) ([]byte, error)

// defaultRunner executes commands via cmdexec.Run with process-group isolation.
func defaultRunner(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	result, err := cmdexec.Run(ctx, name, arguments, cmdexec.Options{})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(string(result.Stderr))
		if stderr != "" {
			return nil, fmt.Errorf("github: %s", stderr)
		}
		return nil, fmt.Errorf("github: exit code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

// execGitHub runs a gh subcommand with the given arguments.
// Unlike execGog, there are no global prefix flags — each tool builds its own
// complete arguments including --json <fields>.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitHub(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Debugf("exec: %s %v", binary, arguments)

	output, err := runner(ctx, binary, arguments...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("github: GitHub authentication required. Please run 'gh auth login' to authenticate")
		}
		return "", fmt.Errorf("github: gh command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates a GitHub authentication problem.
func isAuthError(message string) bool {
	authPhrases := []string{
		"not logged into",
		"authentication required",
		"auth login",
		"try authenticating",
		"token expired",
		"invalid token",
	}
	for _, phrase := range authPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
