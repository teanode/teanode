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
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner executes commands via cmdexec.Run with process-group isolation.
func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	result, err := cmdexec.Run(ctx, name, args, cmdexec.Options{})
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		stderr := strings.TrimSpace(string(result.Stderr))
		if stderr != "" {
			return nil, fmt.Errorf("%s", stderr)
		}
		return nil, fmt.Errorf("exit code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

// execGitea runs a tea subcommand with the given arguments.
// Each tool builds its own complete args including --output json.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitea(ctx context.Context, runner commandRunner, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Debugf("exec: %s %v", binary, args)

	output, err := runner(ctx, binary, args...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("gitea authentication required: please run 'tea login add' to authenticate")
		}
		return "", fmt.Errorf("tea command failed: %s", errorMessage)
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
