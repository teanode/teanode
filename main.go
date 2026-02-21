package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/cmd"
	"github.com/teanode/teanode/internal/configs"
	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:  "teanode",
		Usage: "TeaNode — personal AI assistant gateway",
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
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if value := cmd.String("dir"); value != "" {
				configs.SetDirectory(value)
			}

			level := logging.INFO
			if value := cmd.String("log-level"); value != "" {
				parsed, err := logging.LogLevel(strings.ToUpper(value))
				if err != nil {
					return ctx, fmt.Errorf("invalid log level %q: %w", value, err)
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
			cmd.NewGatewayCommand(),
			cmd.NewRestartCommand(),
			cmd.NewTerminalCommand(),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args); err != nil {
		if errors.Is(err, cmd.ErrRestart) {
			executablePath, executableError := os.Executable()
			if executableError != nil {
				fmt.Fprintf(os.Stderr, "restart failed: %v\n", executableError)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "restarting...")
			execError := syscall.Exec(executablePath, os.Args, os.Environ())
			fmt.Fprintf(os.Stderr, "restart failed: %v\n", execError)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
