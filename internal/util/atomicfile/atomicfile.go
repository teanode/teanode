// Package atomicfile provides helpers to write files atomically via write-and-rename.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/util/security"
)

var log = logging.MustGetLogger("atomicfile")

var (
	ErrInvalidFile = errors.New("atomicfile: invalid file")
)

func Create(filename string) (*os.File, error) {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	tempFilename := filepath.Join(directory, fmt.Sprintf(".%s.%s~", filepath.Base(filename), security.NewULID()))
	log.Debugf("creating temp file at: %s", tempFilename)
	return os.Create(tempFilename)
}

func CommitAs(file *os.File, filename string) error {
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	log.Debugf("committing file: %s", filename)
	return renameReplace(file.Name(), filename)
}

func CommitAsWithMode(file *os.File, filename string, mode os.FileMode) error {
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := CommitAs(file, filename); err != nil {
		return err
	}
	return os.Chmod(filename, mode)
}

func Commit(file *os.File) error {
	tempFilename := file.Name()
	parts := strings.Split(filepath.Base(tempFilename), ".")
	if len(parts) < 3 || parts[0] != "" || !strings.HasSuffix(parts[len(parts)-1], "~") {
		return ErrInvalidFile
	}
	filename := filepath.Join(filepath.Dir(tempFilename), strings.Join(parts[1:len(parts)-1], "."))
	return CommitAs(file, filename)
}

func CommitWithMode(file *os.File, mode os.FileMode) error {
	tempFilename := file.Name()
	parts := strings.Split(filepath.Base(tempFilename), ".")
	if len(parts) < 3 || parts[0] != "" || !strings.HasSuffix(parts[len(parts)-1], "~") {
		return ErrInvalidFile
	}
	filename := filepath.Join(filepath.Dir(tempFilename), strings.Join(parts[1:len(parts)-1], "."))
	return CommitAsWithMode(file, filename, mode)
}

func Discard(file *os.File) error {
	file.Close()
	tempFilename := file.Name()
	parts := strings.Split(filepath.Base(tempFilename), ".")
	if len(parts) < 3 || parts[0] != "" || !strings.HasSuffix(parts[len(parts)-1], "~") {
		return ErrInvalidFile
	}
	if err := os.Remove(tempFilename); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	log.Debugf("discarded temp file: %s", tempFilename)
	return nil
}

func WriteFile(filename string, content []byte) error {
	file, err := Create(filename)
	if err != nil {
		return err
	}
	defer func() {
		_ = Discard(file)
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	return Commit(file)
}

// WriteFileWithMode writes a file atomically and enforces the target mode after
// commit. This is intended for sensitive files such as auth/security material.
func WriteFileWithMode(filename string, content []byte, mode os.FileMode) error {
	file, err := Create(filename)
	if err != nil {
		return err
	}
	defer func() {
		_ = Discard(file)
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	return CommitWithMode(file, mode)
}
