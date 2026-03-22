//go:build !windows

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/urfave/cli/v3"
)

func NewStartCommand() *cli.Command {
	return &cli.Command{
		Name:  "start",
		Usage: "Start the TeaNode node in the background",
		Action: func(ctx context.Context, command *cli.Command) error {
			dataDirectory := DataDirectoryFromContext(ctx)

			// No-op if the node is already running.
			if pid, err := findNodeProcess(ctx); err == nil {
				fmt.Printf("node is already running (pid %d)\n", pid)
				return nil
			}

			// Ensure data directory exists.
			if err := os.MkdirAll(dataDirectory, 0755); err != nil {
				return fmt.Errorf("create data directory: %w", err)
			}

			devNull, err := os.Open(os.DevNull)
			if err != nil {
				return fmt.Errorf("open %s: %w", os.DevNull, err)
			}
			defer func() { _ = devNull.Close() }()

			executablePath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable path: %w", err)
			}

			// Build the argument list: teanode --dir <dataDirectory> node --log-file <path> [extra args...]
			logPath := filepath.Join(dataDirectory, "node.log")
			args := []string{executablePath, "--dir", dataDirectory}
			if logLevel := command.Root().String("log-level"); logLevel != "" {
				args = append(args, "--log-level", logLevel)
			}
			args = append(args, "node", "--log-file", logPath)
			args = append(args, command.Args().Slice()...)

			process, err := os.StartProcess(executablePath, args, &os.ProcAttr{
				Dir: "/",
				Env: os.Environ(),
				Files: []*os.File{
					devNull, // stdin
					devNull, // stdout
					devNull, // stderr
				},
				Sys: &syscall.SysProcAttr{
					Setsid: true,
				},
			})
			if err != nil {
				return fmt.Errorf("start node process: %w", err)
			}

			fmt.Printf("node started (pid %d), logging to %s\n", process.Pid, logPath)
			_ = process.Release()
			return nil
		},
	}
}
