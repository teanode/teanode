package confluence

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
			return nil, fmt.Errorf("confluence: %s", stderr)
		}
		return nil, fmt.Errorf("confluence: exit code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

// execConfluence runs a confluence subcommand.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execConfluence(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Debugf("exec: %s %v", binary, arguments)

	output, err := runner(ctx, binary, arguments...)
	if err != nil {
		errorMessage := err.Error()
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("confluence: confluence authentication required. please run 'confluence init' to configure")
		}
		return "", fmt.Errorf("confluence: confluence command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates a Confluence authentication problem.
func isAuthError(message string) bool {
	authPhrases := []string{
		"not authenticated",
		"auth required",
		"token expired",
		"invalid token",
		"unauthorized",
		"401",
		"no configuration found",
		"missing api token",
	}
	for _, phrase := range authPhrases {
		if strings.Contains(strings.ToLower(message), phrase) {
			return true
		}
	}
	return false
}

// wrapPlainOutput wraps non-JSON command output in a JSON envelope.
func wrapPlainOutput(status string, output string) string {
	return fmt.Sprintf(`{"status":%q,"message":%q}`, status, output)
}
