//go:build !windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func restartProcess(ctx context.Context) error {
	pid, err := findNodeProcess(ctx)
	if err != nil {
		return err
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			pidFilename := filepath.Join(DataDirectoryFromContext(ctx), "node.pid")
			if removeErr := os.Remove(pidFilename); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				log.Warningf("failed to remove stale node pid file %s: %v", pidFilename, removeErr)
			}
		}
		return fmt.Errorf("cmd: failed to signal node process %d: %w", pid, err)
	}

	return nil
}
