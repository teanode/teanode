//go:build windows

package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func NewStartCommand() *cli.Command {
	return &cli.Command{
		Name:  "start",
		Usage: "Start the TeaNode node in the background (Linux/macOS only)",
		Action: func(ctx context.Context, command *cli.Command) error {
			return fmt.Errorf("start command is only supported on Linux and macOS")
		},
	}
}
