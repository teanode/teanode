package cmd

import (
	"context"
	"fmt"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/teanode/teanode/internal/updater"
	"github.com/teanode/teanode/internal/version"
)

// NewUpdateCommand creates the "update" CLI command for checking and applying
// self-updates from GitHub Releases.
func NewUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Check for and apply TeaNode updates",
		Commands: []*cli.Command{
			newUpdateCheckCommand(),
			newUpdateApplyCommand(),
		},
		// Default action (no subcommand) behaves like "check".
		Action: func(ctx context.Context, command *cli.Command) error {
			return updateCheck(ctx)
		},
	}
}

func newUpdateCheckCommand() *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Check for available updates",
		Action: func(ctx context.Context, command *cli.Command) error {
			return updateCheck(ctx)
		},
	}
}

func newUpdateApplyCommand() *cli.Command {
	return &cli.Command{
		Name:  "apply",
		Usage: "Download and apply the latest update",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "apply even in container environments",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			return updateApply(ctx, command.Bool("force"))
		},
	}
}

func updateCheck(ctx context.Context) error {
	fmt.Printf("TeaNode %s (%s/%s)\n", version.Version(), runtime.GOOS, runtime.GOARCH)

	if updater.IsContainerEnvironment() {
		fmt.Println("Note: container environment detected. Self-update is not recommended.")
	}

	fmt.Println("Checking for updates...")

	release, err := updater.CheckLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("cmd: check failed: %w", err)
	}

	newer, err := updater.IsNewer(release.Version(), version.Version())
	if err != nil {
		return fmt.Errorf("cmd: version comparison: %w", err)
	}

	if !newer {
		fmt.Printf("You are running the latest version (%s).\n", version.Version())
		return nil
	}

	fmt.Printf("Update available: %s → %s\n", version.Version(), release.Version())
	fmt.Printf("Release: %s\n", release.HTMLURL)

	archiveName := updater.PlatformAssetName(release.Version())
	asset := release.FindAsset(archiveName)
	if asset == nil {
		fmt.Printf("Warning: no release asset found for %s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, archiveName)
	} else {
		fmt.Printf("Asset: %s (%.1f MB)\n", asset.Name, float64(asset.Size)/(1<<20))
	}

	fmt.Println("\nRun 'teanode update apply' to download and install the update.")
	return nil
}

func updateApply(ctx context.Context, force bool) error {
	fmt.Printf("TeaNode %s (%s/%s)\n", version.Version(), runtime.GOOS, runtime.GOARCH)

	if updater.IsContainerEnvironment() && !force {
		return fmt.Errorf("cmd: container environment detected; self-update is not supported here (use --force to override)")
	}
	if err := updater.ValidateApplyEnvironment(); err != nil {
		return err
	}

	fmt.Println("Checking for updates...")

	release, err := updater.CheckLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("cmd: check failed: %w", err)
	}

	newer, err := updater.IsNewer(release.Version(), version.Version())
	if err != nil {
		return fmt.Errorf("cmd: version comparison: %w", err)
	}

	if !newer {
		fmt.Printf("Already running the latest version (%s). Nothing to do.\n", version.Version())
		return nil
	}

	fmt.Printf("Downloading %s → %s...\n", version.Version(), release.Version())

	result, err := updater.DownloadAndVerify(ctx, release)
	if err != nil {
		return fmt.Errorf("cmd: download/verify failed: %w", err)
	}

	fmt.Printf("Verified (SHA256: %s)\n", result.Checksum)
	fmt.Println("Applying update...")

	if err := updater.Apply(result.StagedPath); err != nil {
		return fmt.Errorf("cmd: apply failed: %w", err)
	}

	fmt.Printf("Successfully updated to %s.\n", release.Version())
	fmt.Println("Restart TeaNode to use the new version.")
	return nil
}
