//go:build windows

package updater

import (
	"fmt"
)

// platformApply rejects in-place self-update on Windows until a helper-based
// replacement flow exists. Replacing the running binary directly is not
// reliable there and can leave the installation in a broken state.
func platformApply(currentPath, stagedPath string) error {
	return fmt.Errorf("self-update apply is not supported on Windows yet")
}
