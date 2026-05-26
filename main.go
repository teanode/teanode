package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/cmd"
	"github.com/urfave/cli/v3"
)

func main() {
	// Resolve the executable path once at startup, before any self-update can
	// replace the binary. On Linux, os.Executable() reads /proc/self/exe which
	// follows the inode — after the updater renames the old binary away and
	// deletes it, /proc/self/exe points to a "(deleted)" path. Capturing the
	// resolved path here ensures the restart target is always valid.
	executablePath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(executablePath); err == nil {
		executablePath = resolved
	}

	app := &cli.Command{
		Name:  "teanode",
		Usage: "TeaNode — personal AI assistant node",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "dir",
				Aliases: []string{"d"},
				Usage:   "data directory (default ~/.teanode)",
				Sources: cli.EnvVars("TEANODE_DIR"),
			},
			&cli.StringFlag{
				Name:    "log-level",
				Aliases: []string{"l"},
				Usage:   "log level (DEBUG, INFO, WARNING, ERROR, CRITICAL)",
				Sources: cli.EnvVars("TEANODE_LOG_LEVEL"),
			},
		},
		Before: func(ctx context.Context, command *cli.Command) (context.Context, error) {
			dataDirectory, err := cmd.ResolveDataDirectory(command.String("dir"))
			if err != nil {
				return ctx, err
			}
			ctx = cmd.ContextWithDataDirectory(ctx, dataDirectory)

			level := logging.INFO
			if value := command.String("log-level"); value != "" {
				parsed, err := logging.LogLevel(strings.ToUpper(value))
				if err != nil {
					return ctx, fmt.Errorf("main: invalid log level %q: %w", value, err)
				}
				level = parsed
			}
			format := logging.MustStringFormatter(
				"%{color}%{time:2006-01-02 15:04:05.000000} %{module} [%{level}] <%{pid}> [%{shortfile} %{shortfunc}] %{message}%{color:reset}",
			)
			backend := logging.NewLogBackend(os.Stderr, "", 0)
			formatted := logging.NewBackendFormatter(backend, format)
			leveled := logging.AddModuleLevel(formatted)
			leveled.SetLevel(level, "")
			logging.SetBackend(leveled)
			return ctx, nil
		},
		Commands: []*cli.Command{
			cmd.NewNodeCommand(),
			cmd.NewStartCommand(),
			cmd.NewStopCommand(),
			cmd.NewStatusCommand(),
			cmd.NewRestartCommand(),
			cmd.NewTerminalCommand(),
			cmd.NewUpdateCommand(),
			cmd.NewVersionCommand(),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args); err != nil {
		if errors.Is(err, cmd.ErrRestart) {
			fmt.Fprintln(os.Stderr, "restarting...")
			execError := syscall.Exec(executablePath, os.Args, os.Environ())
			fmt.Fprintf(os.Stderr, "restart failed: %v\n", execError)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
