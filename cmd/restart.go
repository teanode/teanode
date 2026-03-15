package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func NewRestartCommand() *cli.Command {
	return &cli.Command{
		Name:  "restart",
		Usage: "Restart a running TeaNode node process",
		Action: func(ctx context.Context, command *cli.Command) error {
			if err := restartProcess(ctx); err != nil {
				return err
			}
			fmt.Println("restart signal sent")
			return nil
		},
	}
}
