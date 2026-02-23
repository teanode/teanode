package google

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	maxOutputBytes = 50 * 1024 // 50 KB output truncation limit
	execTimeout    = 60 * time.Second
)

// commandRunner abstracts command execution for testing.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// defaultRunner executes commands via os/exec.
func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		// Return stderr content as the error message for better diagnostics.
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s", stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// execGog runs a gog subcommand with --json --no-input --results-only flags.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGog(ctx context.Context, runner commandRunner, binary string, account string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	fullArgs := []string{"--json", "--no-input", "--results-only"}
	if account != "" {
		fullArgs = append(fullArgs, "--account", account)
	}
	fullArgs = append(fullArgs, args...)

	log.Debugf("exec: %s %v", binary, fullArgs)

	output, err := runner(ctx, binary, fullArgs...)
	if err != nil {
		errMsg := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errMsg) {
			return "", fmt.Errorf("Google authentication required. Please run 'gog auth login' to authenticate")
		}
		return "", fmt.Errorf("gog command failed: %s", errMsg)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates an authentication problem.
func isAuthError(msg string) bool {
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
		if bytes.Contains([]byte(msg), []byte(phrase)) {
			return true
		}
	}
	return false
}
