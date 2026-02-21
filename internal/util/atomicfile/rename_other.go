//go:build !windows

package atomicfile

import "os"

func renameReplace(source string, destination string) error {
	return os.Rename(source, destination)
}
