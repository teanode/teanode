package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("shell")

const (
	defaultTimeout = 120 * time.Second
	maxTimeout     = 600 * time.Second
	maxOutputBytes = 256 * 1024 // 256 KB per stream
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&shellTool{}}
	})
}

type shellTool struct{}

func (self *shellTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "shell",
			Description: "Execute a shell command on the local machine. The command runs via sh -c. Non-zero exit codes are reported in the result, not as errors.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to execute.",
					},
					"directory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for the command. Defaults to the user's home directory.",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (default 120, max 600).",
					},
					"environment": map[string]interface{}{
						"type":        "object",
						"description": "Extra environment variables as key-value pairs.",
					},
				},
				"required": []string{"command"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"stdout": map[string]interface{}{
						"type":        "string",
						"description": "Standard output from the command.",
					},
					"stderr": map[string]interface{}{
						"type":        "string",
						"description": "Standard error from the command.",
					},
					"exitCode": map[string]interface{}{
						"type":        "integer",
						"description": "Exit code of the command.",
					},
					"stdoutTruncated": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether stdout was truncated.",
					},
					"stderrTruncated": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether stderr was truncated.",
					},
					"timedOut": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the command timed out.",
					},
					"duration": map[string]interface{}{
						"type":        "number",
						"description": "Execution duration in seconds.",
					},
				},
			},
		},
	}
}

func (self *shellTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Command     string            `json:"command"`
		Directory   string            `json:"directory"`
		Timeout     int               `json:"timeout"`
		Environment map[string]string `json:"environment"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Resolve timeout.
	timeout := defaultTimeout
	if arguments.Timeout > 0 {
		timeout = time.Duration(arguments.Timeout) * time.Second
		if timeout > maxTimeout {
			timeout = maxTimeout
		}
	}

	// Resolve working directory.
	directory := arguments.Directory
	if directory == "" {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("getting home directory: %w", err)
		}
		directory = homeDirectory
	}

	log.Debugf("exec: sh -c %q in %s (timeout %s)", arguments.Command, directory, timeout)

	// Create command with timeout context.
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(commandContext, "sh", "-c", arguments.Command)
	command.Dir = directory
	command.Stdin = nil // Reads return EOF immediately.
	command.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // New session with no controlling terminal; prevents /dev/tty access (e.g. sudo password prompts).
	}

	// Set environment variables.
	if len(arguments.Environment) > 0 {
		command.Env = os.Environ()
		for key, value := range arguments.Environment {
			command.Env = append(command.Env, key+"="+value)
		}
	}

	// Capture stdout and stderr separately.
	var stdoutBuffer, stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer

	startTime := time.Now()
	err := command.Run()
	duration := time.Since(startTime).Seconds()

	// Determine if we timed out.
	timedOut := commandContext.Err() == context.DeadlineExceeded

	// Extract exit code.
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else if timedOut {
			exitCode = -1
		} else {
			// Command failed to start entirely.
			return "", fmt.Errorf("executing command: %w", err)
		}
	}

	// Truncate output if needed.
	stdout := stdoutBuffer.Bytes()
	stdoutTruncated := len(stdout) > maxOutputBytes
	if stdoutTruncated {
		stdout = stdout[:maxOutputBytes]
	}

	stderr := stderrBuffer.Bytes()
	stderrTruncated := len(stderr) > maxOutputBytes
	if stderrTruncated {
		stderr = stderr[:maxOutputBytes]
	}

	result, err := json.Marshal(map[string]interface{}{
		"stdout":          string(stdout),
		"stderr":          string(stderr),
		"exitCode":        exitCode,
		"stdoutTruncated": stdoutTruncated,
		"stderrTruncated": stderrTruncated,
		"timedOut":        timedOut,
		"duration":        duration,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}
