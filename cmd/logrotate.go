package cmd

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxRotatedLogs = 5

// rotateLogFile rotates the log file at the given path.
// The rotation scheme is: node.log -> node.log.1 -> node.log.2.gz -> node.log.3.gz -> ...
// Files numbered .2 and above are gzip-compressed.
func rotateLogFile(logPath string) error {
	// Remove the oldest rotated log.
	_ = os.Remove(fmt.Sprintf("%s.%d.gz", logPath, maxRotatedLogs))

	// Shift compressed logs (.2.gz and above) up by one.
	for i := maxRotatedLogs - 1; i >= 2; i-- {
		src := fmt.Sprintf("%s.%d.gz", logPath, i)
		dst := fmt.Sprintf("%s.%d.gz", logPath, i+1)
		if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rotate log %s -> %s: %w", src, dst, err)
		}
	}

	// Compress .1 into .2.gz.
	log1 := logPath + ".1"
	if _, err := os.Stat(log1); err == nil {
		if err := compressFile(log1, logPath+".2.gz"); err != nil {
			return fmt.Errorf("compress rotated log: %w", err)
		}
		if err := os.Remove(log1); err != nil {
			return fmt.Errorf("remove compressed source: %w", err)
		}
	}

	// Move current log to .1 (uncompressed, most recent rotated).
	if _, err := os.Stat(logPath); err == nil {
		if err := os.Rename(logPath, log1); err != nil {
			return fmt.Errorf("rotate current log: %w", err)
		}
	}

	return nil
}

// compressFile gzip-compresses src into dst.
func compressFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	gz := gzip.NewWriter(out)

	if _, err := io.Copy(gz, in); err != nil {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}

	if err := gz.Close(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}

	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}

	return nil
}
