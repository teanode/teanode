package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
)

var errInvalidPidFile = errors.New("cmd: invalid pid file")

type pidGuard struct {
	path string
	pid  int
}

func acquirePidGuard(ctx context.Context) (*pidGuard, error) {
	pidFilename := filepath.Join(DataDirectoryFromContext(ctx), "node.pid")

	existingPid, err := readPidFile(pidFilename)
	switch {
	case err == nil:
		if processExists(existingPid) {
			return nil, fmt.Errorf("cmd: node already running (pid %d)", existingPid)
		}
		log.Warningf("removing stale node pid file at %s (pid %d not running)", pidFilename, existingPid)
		if err := os.Remove(pidFilename); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cmd: remove stale node pid file: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		// No existing pid file.
	case errors.Is(err, errInvalidPidFile):
		log.Warningf("removing invalid node pid file at %s: %v", pidFilename, err)
		if err := os.Remove(pidFilename); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cmd: remove invalid node pid file: %w", err)
		}
	default:
		return nil, fmt.Errorf("cmd: read node pid file: %w", err)
	}

	currentPid := os.Getpid()
	if err := atomicfile.WriteFile(pidFilename, []byte(strconv.Itoa(currentPid)+"\n")); err != nil {
		return nil, fmt.Errorf("cmd: write node pid file: %w", err)
	}
	return &pidGuard{path: pidFilename, pid: currentPid}, nil
}

func (self *pidGuard) Release() error {
	currentPid, err := readPidFile(self.path)
	switch {
	case err == nil:
		if currentPid != self.pid {
			return nil
		}
	case errors.Is(err, os.ErrNotExist):
		return nil
	case errors.Is(err, errInvalidPidFile):
		return nil
	default:
		return err
	}
	if err := os.Remove(self.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// findNodeProcess reads the pid file and returns the pid of a running node process.
// It cleans up stale pid files and returns an error if the node is not running.
func findNodeProcess(ctx context.Context) (int, error) {
	pidFilename := filepath.Join(DataDirectoryFromContext(ctx), "node.pid")

	pid, err := readPidFile(pidFilename)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return 0, fmt.Errorf("cmd: node is not running (pid file not found: %s)", pidFilename)
	case errors.Is(err, errInvalidPidFile):
		return 0, fmt.Errorf("cmd: node pid file is invalid: %s", pidFilename)
	default:
		return 0, fmt.Errorf("cmd: read node pid file: %w", err)
	}

	if !processExists(pid) {
		if removeErr := os.Remove(pidFilename); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Warningf("failed to remove stale node pid file %s: %v", pidFilename, removeErr)
		}
		return 0, fmt.Errorf("cmd: node is not running (stale pid file removed: %s)", pidFilename)
	}

	return pid, nil
}

func readPidFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("cmd: %w: %q", errInvalidPidFile, value)
	}
	return pid, nil
}
