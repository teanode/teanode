//go:build !windows

package updater

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// platformApply replaces the current executable on Unix systems.
// Strategy: rename current → .bak, rename staged → current, remove .bak on success.
// Falls back to copy when staged binary is on a different filesystem (EXDEV).
func platformApply(currentPath, stagedPath string) error {
	backupPath := currentPath + ".bak"

	// Remove any stale backup from a prior update.
	_ = os.Remove(backupPath)

	// Backup current binary.
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Move staged binary into place. If the staged binary lives on a
	// different filesystem (common when /tmp is tmpfs), rename returns
	// EXDEV. In that case fall back to a copy-and-remove.
	if err := renameOrCopy(stagedPath, currentPath); err != nil {
		// Restore backup on failure.
		if restoreError := os.Rename(backupPath, currentPath); restoreError != nil {
			return fmt.Errorf("CRITICAL: failed to restore backup after apply error: apply=%w, restore=%v", err, restoreError)
		}
		return fmt.Errorf("applying update: %w (backup restored)", err)
	}

	// Clean up backup. Failure here is non-fatal.
	_ = os.Remove(backupPath)

	return nil
}

// renameOrCopy attempts os.Rename first; if that fails with EXDEV (cross-device
// link) it falls back to copying the file contents and removing the source.
func renameOrCopy(source, destination string) error {
	err := os.Rename(source, destination)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	// Cross-device: copy contents then remove source.
	return copyFile(source, destination)
}

// copyFile copies source to destination preserving permissions, then removes
// the source. It writes to a temporary file in the destination directory first
// and renames it into place so an incomplete copy never appears at destination.
func copyFile(source, destination string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("opening staged binary: %w", err)
	}
	defer func() { _ = sourceFile.Close() }()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("stat staged binary: %w", err)
	}

	// Write to a temp file in the same directory as destination so the
	// final rename is always same-device.
	temporaryFile, err := os.CreateTemp(filepath.Dir(destination), ".teanode-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file for copy: %w", err)
	}
	temporaryPath := temporaryFile.Name()

	if _, err := io.Copy(temporaryFile, sourceFile); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("copying staged binary: %w", err)
	}
	if err := temporaryFile.Chmod(sourceInfo.Mode()); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Same-device rename: atomic.
	if err := os.Rename(temporaryPath, destination); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("moving temp file into place: %w", err)
	}

	// Remove staged source. Non-fatal if it fails.
	_ = os.Remove(source)

	return nil
}
