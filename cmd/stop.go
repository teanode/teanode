//go:build !windows

package cmd

import (
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
)

func NewStopCommand() *cli.Command {
	return &cli.Command{
		Name:  "stop",
		Usage: "Stop a running TeaNode node process",
		Action: func(ctx context.Context, command *cli.Command) error {
			pid, err := findNodeProcess(ctx)
			if err != nil {
				return err
			}

			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("cmd: failed to signal node process %d: %w", pid, err)
			}

			fmt.Printf("stop signal sent to node process %d\n", pid)

			// Wait for the process to exit.
			deadline := time.After(30 * time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-deadline:
					return fmt.Errorf("cmd: node process %d did not exit within 30 seconds", pid)
				case <-ticker.C:
					if !processExists(pid) {
						fmt.Println("node stopped")
						return nil
					}
				}
			}
		},
	}
}
