//go:build windows

package cmd

import (
	"context"
	"fmt"
	"os"
)

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, FindProcess always succeeds. Signal 0 is not supported,
	// so we rely on the process handle being valid.
	_ = process.Release()
	return true
}

func restartProcess(ctx context.Context) error {
	return fmt.Errorf("restart command is only supported on Linux and macOS")
}
