package mattermost

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
			return nil, fmt.Errorf("mattermost: %s", stderr)
		}
		return nil, fmt.Errorf("mattermost: exit code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

// execMattermostWithTeam runs an mm subcommand with --json and optional -T team flags.
func execMattermostWithTeam(ctx context.Context, runner commandRunner, binary string, team string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	fullArguments := []string{"--json"}
	if team != "" {
		fullArguments = append(fullArguments, "-T", team)
	}
	fullArguments = append(fullArguments, arguments...)

	log.Debugf("exec: %s %v", binary, fullArguments)

	output, err := runner(ctx, binary, fullArguments...)
	if err != nil {
		errorMessage := err.Error()
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("mattermost: mattermost authentication required. please run 'mm auth login' to authenticate")
		}
		return "", fmt.Errorf("mattermost: mm command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// execMattermost runs an mm subcommand with --json flag.
// It enforces a timeout and truncates output exceeding maxOutputBytes.
func execMattermost(ctx context.Context, runner commandRunner, binary string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	fullArguments := []string{"--json"}
	fullArguments = append(fullArguments, arguments...)

	log.Debugf("exec: %s %v", binary, fullArguments)

	output, err := runner(ctx, binary, fullArguments...)
	if err != nil {
		errorMessage := err.Error()
		if isAuthError(errorMessage) {
			return "", fmt.Errorf("mattermost: mattermost authentication required. please run 'mm auth login' to authenticate")
		}
		return "", fmt.Errorf("mattermost: mm command failed: %s", errorMessage)
	}

	result := string(output)
	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n... (output truncated)"
	}

	return result, nil
}

// isAuthError checks if an error message indicates a Mattermost authentication problem.
func isAuthError(message string) bool {
	authPhrases := []string{
		"not logged in",
		"not authenticated",
		"auth required",
		"token expired",
		"invalid token",
		"no active profile",
		"unauthorized",
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
