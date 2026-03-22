//go:build windows

package cmdexec

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Run executes a command. On Windows the process is started in a new process
// group and killed directly on context cancellation.
func Run(ctx context.Context, name string, arguments []string, options Options) (*Result, error) {
	directory := options.Directory
	if strings.HasPrefix(directory, "~/") || directory == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			directory = home + directory[1:]
		}
	}

	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	command.Cancel = func() error {
		if command.Process != nil {
			return command.Process.Kill()
		}
		return nil
	}

	waitDelay := options.WaitDelay
	if waitDelay <= 0 {
		waitDelay = defaultWaitDelay
	}
	command.WaitDelay = waitDelay

	if options.Stdin != "" {
		command.Stdin = strings.NewReader(options.Stdin)
	}

	if len(options.Environment) > 0 {
		command.Env = append(command.Environ(), options.Environment...)
	}

	var stdoutBuffer, stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer

	err := command.Run()

	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			return &Result{
				Stdout:   stdoutBuffer.Bytes(),
				Stderr:   stderrBuffer.Bytes(),
				ExitCode: -1,
			}, err
		}
	}

	return &Result{
		Stdout:   stdoutBuffer.Bytes(),
		Stderr:   stderrBuffer.Bytes(),
		ExitCode: exitCode,
	}, nil
}
