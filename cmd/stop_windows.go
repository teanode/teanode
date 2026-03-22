//go:build windows

package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func NewStopCommand() *cli.Command {
	return &cli.Command{
		Name:  "stop",
		Usage: "Stop a running TeaNode node process (Linux/macOS only)",
		Action: func(ctx context.Context, command *cli.Command) error {
			return fmt.Errorf("stop command is only supported on Linux and macOS")
		},
	}
}
