package cmd

import (
	"context"
	"fmt"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/teanode/teanode/internal/version"
)

// NewVersionCommand creates the "version" CLI command.
func NewVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print TeaNode version information",
		Action: func(ctx context.Context, command *cli.Command) error {
			fmt.Printf("TeaNode %s (%s) %s/%s\n",
				version.Version(), version.Commit(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
