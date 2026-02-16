// Package version exposes build-time version info injected via ldflags.
//
//	go build -ldflags "-X github.com/teanode/teanode/internal/version.version=0.1.0
//	                    -X github.com/teanode/teanode/internal/version.commit=abc1234"
package version

import "fmt"

// version and commit are set at build time via -ldflags.
var (
	version = "0.1.0"
	commit = "unknown"
)

// Version returns the semantic version string (e.g. "0.1.0").
func Version() string { return version }

// Commit returns the short git commit hash (e.g. "abc1234").
func Commit() string { return commit }

// ServerName returns a combined identifier suitable for the Server HTTP header
// (e.g. "TeaNode/0.1.0-abc1234").
func ServerName() string {
	return fmt.Sprintf("TeaNode/%s+%s", version, commit)
}
