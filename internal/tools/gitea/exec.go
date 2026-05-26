package gitea

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
			return nil, fmt.Errorf("gitea: %s", stderr)
		}
		return nil, fmt.Errorf("gitea: exit code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

// execGitea runs a tea subcommand with the given arguments.
// Each tool builds its own complete arguments including --output json.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitea(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Debugf("exec: %s %v", binary, arguments)

	output, err := runner(ctx, binary, arguments...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("gitea: gitea authentication required: please run 'tea login add' to authenticate")
		}
		return "", fmt.Errorf("gitea: tea command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates a Gitea authentication problem.
func isAuthError(message string) bool {
	authPhrases := []string{
		"No login configured",
		"tea login add",
		"401 Unauthorized",
		"token is required",
		"unauthorized",
	}
	for _, phrase := range authPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
