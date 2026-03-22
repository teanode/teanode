//go:build windows

package cmd

import "context"

func startLogRotation(ctx context.Context, logPath string) {
	// Log rotation with fd redirection is not supported on Windows.
}
