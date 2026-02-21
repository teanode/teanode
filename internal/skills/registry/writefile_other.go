//go:build !(linux || darwin)

package registry

import (
	"path/filepath"

	"github.com/teanode/teanode/internal/util/atomicfile"
)

func writeInstalledFile(directory string, filename string, content []byte) error {
	return atomicfile.WriteFile(filepath.Join(directory, filename), content)
}

