//go:build !windows

package cmd

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"github.com/teanode/teanode/internal/util/deferutil"
	"io"
	"os"
	"time"
)

const maxRotatedLogs = 5

// maxLogFileSize is the size threshold (50 MB) at which the log file is rotated.
const maxLogFileSize = 50 * 1024 * 1024

// rotateLogFile rotates the log file at the given path.
// The rotation scheme is: node.log -> node.log.1 -> node.log.2.gz -> node.log.3.gz -> ...
// Files numbered .2 and above are gzip-compressed.
func rotateLogFile(logPath string) error {
	// Remove the oldest rotated log.
	_ = os.Remove(fmt.Sprintf("%s.%d.gz", logPath, maxRotatedLogs))

	// Shift compressed logs (.2.gz and above) up by one.
	for rotationIndex := maxRotatedLogs - 1; rotationIndex >= 2; rotationIndex-- {
		source := fmt.Sprintf("%s.%d.gz", logPath, rotationIndex)
		destination := fmt.Sprintf("%s.%d.gz", logPath, rotationIndex+1)
		if err := os.Rename(source, destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cmd: rotate log %s -> %s: %w", source, destination, err)
		}
	}

	// Compress .1 into .2.gz.
	log1 := logPath + ".1"
	if _, err := os.Stat(log1); err == nil {
		if err := compressFile(log1, logPath+".2.gz"); err != nil {
			return fmt.Errorf("cmd: compress rotated log: %w", err)
		}
		if err := os.Remove(log1); err != nil {
			return fmt.Errorf("cmd: remove compressed source: %w", err)
		}
	}

	// Move current log to .1 (uncompressed, most recent rotated).
	if _, err := os.Stat(logPath); err == nil {
		if err := os.Rename(logPath, log1); err != nil {
			return fmt.Errorf("cmd: rotate current log: %w", err)
		}
	}

	return nil
}

// startLogRotation runs a background goroutine that periodically checks the log
// file size and rotates it when it exceeds maxLogFileSize. After rotation, it
// reopens the log file and redirects stdout and stderr to the new file.
func startLogRotation(ctx context.Context, logPath string) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer deferutil.Recover()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				info, err := os.Stat(logPath)
				if err != nil || info.Size() < maxLogFileSize {
					continue
				}
				if err := rotateLogFile(logPath); err != nil {
					log.Errorf("log rotation failed: %v", err)
					continue
				}
				// Reopen the log file and redirect stdout/stderr to it.
				file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if err != nil {
					log.Errorf("reopen log file after rotation: %v", err)
					continue
				}
				fd := int(file.Fd())
				_ = dup2(fd, int(os.Stdout.Fd()))
				_ = dup2(fd, int(os.Stderr.Fd()))
				_ = file.Close()
				log.Infof("log rotated: %s", logPath)
			}
		}
	}()
}

// compressFile gzip-compresses source into destination.
func compressFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(destination)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)

	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}

	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(destination)
		return err
	}

	return nil
}
