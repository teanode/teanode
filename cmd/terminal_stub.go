//go:build !linux

package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

// NewTerminalCommand is unavailable on non-Linux platforms.
func NewTerminalCommand() *cli.Command {
	return &cli.Command{
		Name:  "terminal",
		Usage: "Launch a PTY-backed terminal and relay it to the gateway (Linux only)",
		Action: func(ctx context.Context, command *cli.Command) error {
			return fmt.Errorf("terminal command is only supported on Linux")
		},
	}
}

