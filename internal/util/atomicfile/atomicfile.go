// Package atomicfile provides helpers to write files atomically via write-and-rename.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/util/security"
)

var log = logging.MustGetLogger("atomicfile")

var (
	ErrInvalidFile = errors.New("atomicfile: invalid file")
)

func Create(filename string) (*os.File, error) {
	directory := path.Dir(filename)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	tempFilename := path.Join(directory, fmt.Sprintf(".%s.%s~", path.Base(filename), security.NewULID()))
	log.Debugf("creating temp file at: %s", tempFilename)
	return os.Create(tempFilename)
}

func CommitAs(file *os.File, filename string) error {
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	log.Debugf("committing file: %s", filename)
	return os.Rename(file.Name(), filename)
}

func Commit(file *os.File) error {
	tempFilename := file.Name()
	parts := strings.Split(path.Base(tempFilename), ".")
	if len(parts) < 3 || parts[0] != "" || !strings.HasSuffix(parts[len(parts)-1], "~") {
		return ErrInvalidFile
	}
	filename := path.Join(path.Dir(tempFilename), strings.Join(parts[1:len(parts)-1], "."))
	return CommitAs(file, filename)
}

func Discard(file *os.File) error {
	file.Close()
	tempFilename := file.Name()
	parts := strings.Split(path.Base(tempFilename), ".")
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
