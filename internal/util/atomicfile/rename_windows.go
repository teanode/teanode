//go:build windows

package atomicfile

import (
	"errors"
	"os"
)

func renameReplace(source string, destination string) error {
	// Windows rename does not replace existing destination.
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}
