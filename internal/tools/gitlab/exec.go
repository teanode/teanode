package gitlab

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
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Return stderr content as the error message for better diagnostics.
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s", stderr.String())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// execGitLab runs a glab subcommand with the given arguments.
// Each tool builds its own complete args including --output json.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execGitLab(ctx context.Context, runner commandRunner, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Debugf("exec: %s %v", binary, args)

	output, err := runner(ctx, binary, args...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("GitLab authentication required. Please run 'glab auth login' to authenticate")
		}
		return "", fmt.Errorf("glab command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates a GitLab authentication problem.
func isAuthError(message string) bool {
	authPhrases := []string{
		"not logged into",
		"glab auth login",
		"401 Unauthorized",
		"Invalid token",
		"token was revoked",
		"none of the git remotes configured",
	}
	for _, phrase := range authPhrases {
		if bytes.Contains([]byte(message), []byte(phrase)) {
			return true
		}
	}
	return false
}
