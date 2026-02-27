// Package cmdexec provides shared subprocess execution with process-group
// management. It consolidates the Setsid / process-group-kill / WaitDelay
// boilerplate used by multiple tool implementations.
package cmdexec

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const defaultWaitDelay = 3 * time.Second

// Options controls optional behaviour of Run.
type Options struct {
	// Directory is the working directory for the subprocess.
	Directory string

	// Stdin is data piped to the process's standard input.
	Stdin string

	// Environment contains extra environment variables in KEY=VALUE form.
	// They are appended to the current process environment.
	Environment []string

	// WaitDelay overrides the default 3-second grace period for I/O pipes
	// to drain after the process group is killed.
	WaitDelay time.Duration
}

// Result captures the output of a completed subprocess.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run executes a command with process-group isolation. The process is started
// in its own session (Setsid) and the entire process group is killed on
// context cancellation. Non-zero exit codes are returned via Result (err is
// nil); only failures to start the process are returned as errors.
func Run(ctx context.Context, name string, arguments []string, options Options) (*Result, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = options.Directory
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
