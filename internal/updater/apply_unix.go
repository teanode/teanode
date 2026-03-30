//go:build !windows

package updater

import (
	"fmt"
	"os"
)

// platformApply replaces the current executable on Unix systems.
// Strategy: rename current → .bak, rename staged → current, remove .bak on success.
func platformApply(currentPath, stagedPath string) error {
	backupPath := currentPath + ".bak"

	// Remove any stale backup from a prior update.
	_ = os.Remove(backupPath)

	// Backup current binary.
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Move staged binary into place.
	if err := os.Rename(stagedPath, currentPath); err != nil {
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
