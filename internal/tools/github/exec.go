package github

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

// execGitHub runs a gh subcommand with the given arguments.
// Unlike execGog, there are no global prefix flags — each tool builds its own
// complete args including --json <fields>.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitHub(ctx context.Context, runner commandRunner, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Debugf("exec: %s %v", binary, args)

	output, err := runner(ctx, binary, args...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("GitHub authentication required. Please run 'gh auth login' to authenticate")
		}
		return "", fmt.Errorf("gh command failed: %s", errorMessage)
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
		if bytes.Contains([]byte(message), []byte(phrase)) {
			return true
		}
	}
	return false
}
