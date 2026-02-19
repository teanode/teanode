//go:build !linux

package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func NewTerminalCommand() *cli.Command {
	return &cli.Command{
		Name:  "terminal",
		Usage: "Launch a PTY-backed terminal and relay it to the gateway",
		Action: func(_ context.Context, _ *cli.Command) error {
			return fmt.Errorf("terminal command is only supported on Linux")
		},
	}
}
