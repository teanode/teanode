// Package cliexec runs external provider CLIs (gh, glab, tea) with shared
// timeout enforcement, output truncation, and auth-error detection. The
// provider-specific strings (error prefix, binary label, auth phrases) are
// supplied via Config so each tool package stays a thin wrapper.
package cliexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/util/cmdexec"
	"github.com/teanode/teanode/internal/util/textsplit"
)

// Per-package logger declaration (mulint_log).
var log = logging.MustGetLogger("cliexec") //nolint:unused

const (
	// MaxOutputBytes is the output truncation limit for CLI results.
	MaxOutputBytes = 50 * 1024 // 50 KB
	execTimeout    = 60 * time.Second
)

// Runner abstracts command execution for testing.
type Runner func(ctx context.Context, name string, arguments ...string) ([]byte, error)

// Config describes provider-specific behavior for Exec.
type Config struct {
	// ErrorPrefix prefixes all returned errors, e.g. "github".
	ErrorPrefix string
	// CommandLabel names the CLI binary in error messages, e.g. "gh".
	CommandLabel string
	// AuthPhrases are error-message substrings that identify authentication failures.
	AuthPhrases []string
	// AuthHelp is the message returned when an authentication failure is detected.
	AuthHelp string
}

// prefixedError builds an error whose message carries the calling provider's
// prefix (e.g. "github: ...") rather than this package's.
func prefixedError(errorPrefix string, format string, formatArguments ...interface{}) error {
	return errors.New(errorPrefix + ": " + fmt.Sprintf(format, formatArguments...))
}

// NewRunner returns a Runner that executes commands via cmdexec.Run with
// process-group isolation, prefixing failure errors with errorPrefix.
func NewRunner(errorPrefix string) Runner {
	return func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
		result, err := cmdexec.Run(ctx, name, arguments, cmdexec.Options{})
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			stderr := strings.TrimSpace(string(result.Stderr))
			if stderr != "" {
				return nil, prefixedError(errorPrefix, "%s", stderr)
			}
			return nil, prefixedError(errorPrefix, "exit code %d", result.ExitCode)
		}
		return result.Stdout, nil
	}
}

// Exec runs a CLI subcommand with the given arguments. It enforces a timeout
// and truncates output exceeding MaxOutputBytes. Each tool builds its own
// complete arguments, including output-format flags.
func Exec(ctx context.Context, config Config, runner Runner, binary string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	output, err := runner(ctx, binary, arguments...)
	if err != nil {
		errorMessage := err.Error()
		// Detect auth errors and return a clear message for the LLM.
		if IsAuthError(errorMessage, config.AuthPhrases) {
			return "", prefixedError(config.ErrorPrefix, "%s", config.AuthHelp)
		}
		return "", prefixedError(config.ErrorPrefix, "%s command failed: %s", config.CommandLabel, errorMessage)
	}

	result := string(output)
	if len(result) > MaxOutputBytes {
		result = textsplit.TruncateUTF8(result, MaxOutputBytes) + "\n... (output truncated)"
	}

	return result, nil
}

// IsAuthError checks whether an error message contains any of the provider's
// authentication-failure phrases.
func IsAuthError(message string, authPhrases []string) bool {
	for _, phrase := range authPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
