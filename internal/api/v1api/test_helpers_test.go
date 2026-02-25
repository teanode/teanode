package v1api

import (
	"testing"

	"github.com/teanode/teanode/internal/configs"
)

func withTempConfigDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })
	return directory
}
