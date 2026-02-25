package media

import (
	"fmt"
	"os"
	"testing"

	"github.com/teanode/teanode/internal/configs"
)

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "teanode-media-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "MkdirTemp failed: %v\n", err)
		os.Exit(1)
	}
	configs.SetDirectory(directory)
	exitCode := m.Run()
	if removeErr := os.RemoveAll(directory); removeErr != nil {
		fmt.Fprintf(os.Stderr, "RemoveAll failed: %v\n", removeErr)
	}
	os.Exit(exitCode)
}
