package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type dataDirectoryContextKey struct{}

// ContextWithDataDirectory enriches a context with the resolved data directory path.
func ContextWithDataDirectory(ctx context.Context, dataDirectory string) context.Context {
	return context.WithValue(ctx, dataDirectoryContextKey{}, dataDirectory)
}

// DataDirectoryFromContext returns the data directory from context, falling back to
func DataDirectoryFromContext(ctx context.Context) string {
	return ctx.Value(dataDirectoryContextKey{}).(string)
}

// ResolveDataDirectory returns the data directory from the given command flag value,
// the TEANODE_DIR environment variable, or ~/.teanode as a default.
func ResolveDataDirectory(commandValue string) (string, error) {
	if commandValue != "" {
		return commandValue, nil
	}
	if value := os.Getenv("TEANODE_DIR"); value != "" {
		return value, nil
	}
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cmd: cannot determine home directory: %w", err)
	}
	return filepath.Join(homeDirectory, ".teanode"), nil
}
