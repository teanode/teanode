package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/atomicfile"
)

var errInvalidPIDFile = errors.New("invalid pid file")

type gatewayPIDGuard struct {
	path string
	pid  int
}

func acquireGatewayPIDGuard() (*gatewayPIDGuard, error) {
	pidFilename := configs.GatewayPIDFilename()

	existingPid, err := readPIDFile(pidFilename)
	switch {
	case err == nil:
		if processExists(existingPid) {
			return nil, fmt.Errorf("gateway already running (pid %d)", existingPid)
		}
		log.Warningf("removing stale gateway pid file at %s (pid %d not running)", pidFilename, existingPid)
		if err := os.Remove(pidFilename); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale gateway pid file: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		// No existing pid file.
	case errors.Is(err, errInvalidPIDFile):
		log.Warningf("removing invalid gateway pid file at %s: %v", pidFilename, err)
		if err := os.Remove(pidFilename); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove invalid gateway pid file: %w", err)
		}
	default:
		return nil, fmt.Errorf("read gateway pid file: %w", err)
	}

	currentPid := os.Getpid()
	if err := atomicfile.WriteFile(pidFilename, []byte(strconv.Itoa(currentPid)+"\n")); err != nil {
		return nil, fmt.Errorf("write gateway pid file: %w", err)
	}
	return &gatewayPIDGuard{path: pidFilename, pid: currentPid}, nil
}

func (self *gatewayPIDGuard) Release() error {
	currentPid, err := readPIDFile(self.path)
	switch {
	case err == nil:
		if currentPid != self.pid {
			return nil
		}
	case errors.Is(err, os.ErrNotExist):
		return nil
	case errors.Is(err, errInvalidPIDFile):
		return nil
	default:
		return err
	}
	if err := os.Remove(self.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func restartGatewayProcess() error {
	pidFilename := configs.GatewayPIDFilename()
	var err error
	pid, err := readPIDFile(pidFilename)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("gateway is not running (pid file not found: %s)", pidFilename)
	case errors.Is(err, errInvalidPIDFile):
		return fmt.Errorf("gateway pid file is invalid: %s", pidFilename)
	default:
		return fmt.Errorf("read gateway pid file: %w", err)
	}

	if !processExists(pid) {
		if removeErr := os.Remove(pidFilename); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Warningf("failed to remove stale gateway pid file %s: %v", pidFilename, removeErr)
		}
		return fmt.Errorf("gateway is not running (stale pid file removed: %s)", pidFilename)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			if removeErr := os.Remove(pidFilename); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				log.Warningf("failed to remove stale gateway pid file %s: %v", pidFilename, removeErr)
			}
		}
		return fmt.Errorf("failed to signal gateway process %d: %w", pid, err)
	}

	return nil
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("%w: %q", errInvalidPIDFile, value)
	}
	return pid, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
