package agents

import (
	"fmt"
	"os"
	"testing"

	"github.com/teanode/teanode/internal/configs"
	storefs "github.com/teanode/teanode/internal/store/fs"
)

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "teanode-agents-tests-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "MkdirTemp failed: %v\n", err)
		os.Exit(1)
	}
	configs.SetDirectory(directory)
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: directory})
	if openError != nil {
		fmt.Fprintf(os.Stderr, "store open failed: %v\n", openError)
		os.Exit(1)
	}
	if migrateError := openedStore.Migrate(); migrateError != nil {
		fmt.Fprintf(os.Stderr, "store migrate failed: %v\n", migrateError)
		_ = openedStore.Close()
		os.Exit(1)
	}
	exitCode := m.Run()
	if closeError := openedStore.Close(); closeError != nil {
		fmt.Fprintf(os.Stderr, "store close failed: %v\n", closeError)
	}
	if removeErr := os.RemoveAll(directory); removeErr != nil {
		fmt.Fprintf(os.Stderr, "RemoveAll failed: %v\n", removeErr)
	}
	os.Exit(exitCode)
}
