//go:build linux || darwin

package registry

import (
	"fmt"

	"github.com/teanode/teanode/internal/util/security"
	"golang.org/x/sys/unix"
)

func writeInstalledFile(directory string, filename string, content []byte) error {
	directoryFd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFd)

	tempFilename := fmt.Sprintf(".%s.%s~", filename, security.NewULID())
	tempFd, err := unix.Openat(directoryFd, tempFilename, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0644)
	if err != nil {
		return err
	}
	for written := 0; written < len(content); {
		count, writeErr := unix.Write(tempFd, content[written:])
		if writeErr != nil {
			unix.Close(tempFd)
			unix.Unlinkat(directoryFd, tempFilename, 0)
			return writeErr
		}
		written += count
	}
	if err := unix.Fsync(tempFd); err != nil {
		unix.Close(tempFd)
		unix.Unlinkat(directoryFd, tempFilename, 0)
		return err
	}
	if err := unix.Close(tempFd); err != nil {
		unix.Unlinkat(directoryFd, tempFilename, 0)
		return err
	}
	if err := unix.Renameat(directoryFd, tempFilename, directoryFd, filename); err != nil {
		unix.Unlinkat(directoryFd, tempFilename, 0)
		return err
	}
	_ = unix.Fsync(directoryFd)
	return nil
}

