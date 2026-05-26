package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ValidateApplyEnvironment reports whether in-place self-update is supported.
func ValidateApplyEnvironment() error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("updater: self-update apply is not supported on Windows yet")
	}
	return nil
}

// Apply replaces the current executable with the staged binary. It creates a
// backup of the current executable alongside it (with a .bak suffix) and
// atomically swaps the new binary into place where possible.
//
// On failure the backup is restored automatically.
func Apply(stagedPath string) error {
	if err := ValidateApplyEnvironment(); err != nil {
		return err
	}
	currentPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("updater: resolving current executable: %w", err)
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return fmt.Errorf("updater: resolving symlinks: %w", err)
	}

	// Safety: refuse to overwrite if the staged binary is suspiciously small.
	stagedInfo, err := os.Stat(stagedPath)
	if err != nil {
		return fmt.Errorf("updater: stat staged binary: %w", err)
	}
	if stagedInfo.Size() < 1<<20 { // < 1 MB is suspicious
		return fmt.Errorf("updater: staged binary too small (%d bytes), refusing to apply", stagedInfo.Size())
	}

	// Safety: verify we can rename files in the executable's directory.
	if err := checkDirectoryWritable(filepath.Dir(currentPath)); err != nil {
		return fmt.Errorf("updater: executable directory not writable: %w", err)
	}

	return platformApply(currentPath, stagedPath)
}

// checkDirectoryWritable verifies that we can create and rename files in the
// given directory, which is the only filesystem operation the rename-based
// apply strategy requires. We must not open the running executable for writing
// because Linux returns ETXTBSY for executables that are in use.
func checkDirectoryWritable(directory string) error {
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("updater: %s is not a directory", directory)
	}

	// Create and immediately remove a temporary file to verify write access.
	tempFile, err := os.CreateTemp(directory, ".teanode-update-check-*")
	if err != nil {
		return fmt.Errorf("updater: cannot write to directory %s: %w", directory, err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	_ = os.Remove(tempPath)
	return nil
}

// IsContainerEnvironment returns true if we appear to be running inside a
// container (Docker, Podman, etc.), where self-update is likely inappropriate.
func IsContainerEnvironment() bool {
	// Check for .dockerenv marker file.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Check cgroup for container indicators.
	data, err := os.ReadFile("/proc/1/cgroup")
	if err == nil {
		content := string(data)
		if containsAny(content, "docker", "kubepods", "containerd", "lxc") {
			return true
		}
	}
	// Check for container environment variable.
	if os.Getenv("container") != "" {
		return true
	}
	return false
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && len(haystack) >= len(needle) {
			for index := 0; index <= len(haystack)-len(needle); index++ {
				if haystack[index:index+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}
