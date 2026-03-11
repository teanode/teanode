// Package trash provides helpers for moving files into a trash directory.
package trash

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Move relocates a file or directory to trashDirectory.
//
// The destination filename is formatted as:
//
//	YYYY-MM-DD-HH-MM-SS.mmm-original-path-and-filename
func Move(path string, trashDirectory string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if trashDirectory == "" {
		return fmt.Errorf("trash directory is required")
	}

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(trashDirectory, 0755); err != nil {
		return fmt.Errorf("creating trash directory: %w", err)
	}

	absolutePath := path
	if value, absErr := filepath.Abs(path); absErr == nil {
		absolutePath = value
	}
	namePath := absolutePath
	if filepath.Base(trashDirectory) == ".trash" {
		rootDirectory := filepath.Dir(trashDirectory)
		if value, absErr := filepath.Abs(rootDirectory); absErr == nil {
			rootDirectory = value
		}
		if relativePath, relErr := filepath.Rel(rootDirectory, absolutePath); relErr == nil && !isDotOrParent(relativePath) {
			namePath = relativePath
		}
	}

	baseName := fmt.Sprintf("%s-%s",
		time.Now().UTC().Format("2006-01-02-15-04-05.000"),
		sanitizePathForName(namePath),
	)
	destination, err := uniquePath(trashDirectory, baseName)
	if err != nil {
		return err
	}

	if err := os.Rename(path, destination); err == nil {
		return nil
	} else if !isCrossDeviceRename(err) {
		return fmt.Errorf("moving to trash: %w", err)
	}

	if err := copyPath(path, destination, info); err != nil {
		return fmt.Errorf("copying to trash: %w", err)
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("removing original after trash copy: %w", err)
	}
	return nil
}

func sanitizePathForName(path string) string {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if volume != "" {
		clean = strings.TrimPrefix(clean, volume)
		volume = strings.TrimSuffix(volume, ":")
	}

	clean = strings.TrimPrefix(clean, string(filepath.Separator))
	clean = strings.ReplaceAll(clean, string(filepath.Separator), "-")
	clean = strings.ReplaceAll(clean, ":", "-")
	if clean == "" || clean == "." {
		clean = "root"
	}
	if volume != "" {
		return volume + "-" + clean
	}
	return clean
}

func isDotOrParent(path string) bool {
	if path == "." || path == ".." {
		return true
	}
	return strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func uniquePath(directory string, baseName string) (string, error) {
	candidate := filepath.Join(directory, baseName)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	}

	for index := 1; ; index++ {
		candidate = filepath.Join(directory, fmt.Sprintf("%s-%d", baseName, index))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
		if index >= 1_000_000 {
			return "", fmt.Errorf("unable to allocate unique trash name for %q", baseName)
		}
	}
}

func isCrossDeviceRename(err error) bool {
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var linkErr *os.LinkError
	return errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV)
}

func copyPath(source string, destination string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}

	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			entryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			sourcePath := filepath.Join(source, entry.Name())
			destinationPath := filepath.Join(destination, entry.Name())
			if err := copyPath(sourcePath, destinationPath, entryInfo); err != nil {
				return err
			}
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}

	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = destinationFile.Close() }()

	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}
	return nil
}
