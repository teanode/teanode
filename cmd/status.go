package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

func NewStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show the status of the TeaNode node process",
		Action: func(ctx context.Context, command *cli.Command) error {
			dataDirectory := DataDirectoryFromContext(ctx)

			pid, err := findNodeProcess(ctx)
			if err != nil {
				fmt.Println("node is not running")
				return nil
			}

			fmt.Printf("node is running (pid %d)\n", pid)
			fmt.Printf("  pid file: %s\n", filepath.Join(dataDirectory, "node.pid"))
			fmt.Printf("  log file: %s\n", filepath.Join(dataDirectory, "node.log"))
			return nil
		},
	}
}
