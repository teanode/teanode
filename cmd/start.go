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
				return fmt.Errorf("cmd: create data directory: %w", err)
			}

			devNull, err := os.Open(os.DevNull)
			if err != nil {
				return fmt.Errorf("cmd: open %s: %w", os.DevNull, err)
			}
			defer func() { _ = devNull.Close() }()

			executablePath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("cmd: resolve executable path: %w", err)
			}

			// Build the argument list: teanode --dir <dataDirectory> node --log-file <path> [extra arguments...]
			logPath := filepath.Join(dataDirectory, "node.log")
			arguments := []string{executablePath, "--dir", dataDirectory}
			if logLevel := command.Root().String("log-level"); logLevel != "" {
				arguments = append(arguments, "--log-level", logLevel)
			}
			arguments = append(arguments, "node", "--log-file", logPath)
			arguments = append(arguments, command.Args().Slice()...)

			process, err := os.StartProcess(executablePath, arguments, &os.ProcAttr{
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
				return fmt.Errorf("cmd: start node process: %w", err)
			}

			fmt.Printf("node started (pid %d), logging to %s\n", process.Pid, logPath)
			_ = process.Release()
			return nil
		},
	}
}
