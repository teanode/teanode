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
		return fmt.Errorf("self-update apply is not supported on Windows yet")
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
		return fmt.Errorf("resolving current executable: %w", err)
	}
	currentPath, err = filepath.EvalSymlinks(currentPath)
	if err != nil {
		return fmt.Errorf("resolving symlinks: %w", err)
	}

	// Safety: refuse to overwrite if the staged binary is suspiciously small.
	stagedInfo, err := os.Stat(stagedPath)
	if err != nil {
		return fmt.Errorf("stat staged binary: %w", err)
	}
	if stagedInfo.Size() < 1<<20 { // < 1 MB is suspicious
		return fmt.Errorf("staged binary too small (%d bytes), refusing to apply", stagedInfo.Size())
	}

	// Safety: verify the current executable is writable.
	if err := checkWritable(currentPath); err != nil {
		return fmt.Errorf("current executable not writable: %w", err)
	}

	return platformApply(currentPath, stagedPath)
}

// checkWritable verifies that we can write to the file's directory (needed for
// rename operations) and that the file itself is writable.
func checkWritable(path string) error {
	directory := filepath.Dir(path)
	info, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", directory)
	}

	// On Windows, file permission bits are not reliable; we try to open for writing.
	if runtime.GOOS == "windows" {
		return nil // Windows apply path handles this differently.
	}

	// On Unix, check that we can write to the file.
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return file.Close()
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
