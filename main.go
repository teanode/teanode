package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	gologging "github.com/op/go-logging"
	"github.com/urfave/cli/v3"
	"github.com/ziyan/teanode/cmd"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/logging"
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
				config.SetDir(value)
			}

			level := gologging.INFO
			if value := cmd.String("log-level"); value != "" {
				parsed, err := gologging.LogLevel(strings.ToUpper(value))
				if err != nil {
					return ctx, fmt.Errorf("invalid log level %q: %w", value, err)
				}
				level = parsed
			}
			logging.Setup(level)
			return ctx, nil
		},
		Commands: []*cli.Command{
			cmd.GatewayCmd(),
			cmd.TerminalCmd(),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
