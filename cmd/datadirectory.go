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
// ResolveDataDirectory("") when not set.
func DataDirectoryFromContext(ctx context.Context) (string, error) {
	if dataDirectory, ok := ctx.Value(dataDirectoryContextKey{}).(string); ok && dataDirectory != "" {
		return dataDirectory, nil
	}
	return ResolveDataDirectory("")
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
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(homeDirectory, ".teanode"), nil
}

// GatewayPIDFilenameFromContext returns the path to the gateway PID file.
func GatewayPIDFilenameFromContext(ctx context.Context) (string, error) {
	dataDirectory, err := DataDirectoryFromContext(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDirectory, "gateway.pid"), nil
}
