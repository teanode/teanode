//go:build !windows

package cmdexec

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Run executes a command with process-group isolation. The process is started
// in its own session (Setsid) and the entire process group is killed on
// context cancellation. Non-zero exit codes are returned via Result (err is
// nil); only failures to start the process are returned as errors.
func Run(ctx context.Context, name string, arguments []string, options Options) (*Result, error) {
	// Expand ~ prefix since Go does not perform shell-style tilde expansion.
	directory := options.Directory
	if strings.HasPrefix(directory, "~/") || directory == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			directory = home + directory[1:]
		}
	}

	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Kill the entire process group on context cancellation so child
	// processes don't outlive the parent.
	command.Cancel = func() error {
		if command.Process != nil {
			return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
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
