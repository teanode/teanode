// Package cmdexec provides shared subprocess execution with process-group
// management. It consolidates the Setsid / process-group-kill / WaitDelay
// boilerplate used by multiple tool implementations.
package cmdexec

import "time"

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
