package google

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
			return nil, fmt.Errorf("google: %s", stderr)
		}
		return nil, fmt.Errorf("google: exit code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

// execGog runs a gog subcommand with --json --no-input --results-only flags.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGog(ctx context.Context, runner commandRunner, binary string, account string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	fullArguments := []string{"--json", "--no-input", "--results-only"}
	if account != "" {
		fullArguments = append(fullArguments, "--account", account)
	}
	fullArguments = append(fullArguments, arguments...)

	log.Debugf("exec: %s %v", binary, fullArguments)

	output, err := runner(ctx, binary, fullArguments...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("google: google authentication required; run 'gog auth login' to authenticate")
		}
		return "", fmt.Errorf("google: gog command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates an authentication problem.
func isAuthError(message string) bool {
	authPhrases := []string{
		"not authenticated",
		"not logged in",
		"auth required",
		"token expired",
		"invalid credentials",
		"login required",
		"no active account",
		"unauthenticated",
	}
	for _, phrase := range authPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
