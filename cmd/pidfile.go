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
	pidFile, err := configs.GatewayPIDFile()
	if err != nil {
		return nil, err
	}

	existingPid, err := readPIDFile(pidFile)
	switch {
	case err == nil:
		if processExists(existingPid) {
			return nil, fmt.Errorf("gateway already running (pid %d)", existingPid)
		}
		log.Warningf("removing stale gateway pid file at %s (pid %d not running)", pidFile, existingPid)
		if err := os.Remove(pidFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale gateway pid file: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		// No existing pid file.
	case errors.Is(err, errInvalidPIDFile):
		log.Warningf("removing invalid gateway pid file at %s: %v", pidFile, err)
		if err := os.Remove(pidFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove invalid gateway pid file: %w", err)
		}
	default:
		return nil, fmt.Errorf("read gateway pid file: %w", err)
	}

	currentPid := os.Getpid()
	if err := atomicfile.WriteFile(pidFile, []byte(strconv.Itoa(currentPid)+"\n")); err != nil {
		return nil, fmt.Errorf("write gateway pid file: %w", err)
	}
	return &gatewayPIDGuard{path: pidFile, pid: currentPid}, nil
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
	pidFile, err := configs.GatewayPIDFile()
	if err != nil {
		return err
	}
	pid, err := readPIDFile(pidFile)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("gateway is not running (pid file not found: %s)", pidFile)
	case errors.Is(err, errInvalidPIDFile):
		return fmt.Errorf("gateway pid file is invalid: %s", pidFile)
	default:
		return fmt.Errorf("read gateway pid file: %w", err)
	}

	if !processExists(pid) {
		if removeErr := os.Remove(pidFile); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Warningf("failed to remove stale gateway pid file %s: %v", pidFile, removeErr)
		}
		return fmt.Errorf("gateway is not running (stale pid file removed: %s)", pidFile)
	}

	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			if removeErr := os.Remove(pidFile); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				log.Warningf("failed to remove stale gateway pid file %s: %v", pidFile, removeErr)
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
