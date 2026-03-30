package updater

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/version"
)

const (
	// maxArchiveSize is the maximum allowed download size (512 MB).
	maxArchiveSize = 512 << 20
	// maxChecksumSize is the maximum allowed checksum file size (64 KB).
	maxChecksumSize = 64 << 10
	// binaryName is the name of the TeaNode binary inside the archive.
	binaryName = "teanode"
)

// DownloadResult holds the results of a successful download and verification.
type DownloadResult struct {
	// StagedPath is the path to the verified binary staged in a temporary directory.
	StagedPath string
	// Checksum is the SHA256 hex digest of the downloaded archive.
	Checksum string
}

// DownloadAndVerify downloads the release archive and checksum file, verifies
// the archive integrity, extracts the binary, and stages it in a temporary
// directory. The caller is responsible for cleaning up the staged directory
// (filepath.Dir of StagedPath) on failure.
func DownloadAndVerify(ctx context.Context, release *ReleaseInfo) (*DownloadResult, error) {
	releaseVersion := release.Version()

	// Find the archive and checksum assets.
	archiveName := PlatformAssetName(releaseVersion)
	archiveAsset := release.FindAsset(archiveName)
	if archiveAsset == nil {
		return nil, fmt.Errorf("release %s has no asset for %s/%s (%s)", releaseVersion, runtime.GOOS, runtime.GOARCH, archiveName)
	}

	checksumName := ChecksumAssetName(releaseVersion)
	checksumAsset := release.FindAsset(checksumName)
	if checksumAsset == nil {
		return nil, fmt.Errorf("release %s has no checksum file (%s)", releaseVersion, checksumName)
	}

	// Download checksum file.
	checksumData, err := downloadToMemory(ctx, checksumAsset.BrowserDownloadURL, maxChecksumSize)
	if err != nil {
		return nil, fmt.Errorf("downloading checksum file: %w", err)
	}

	expectedChecksum, err := parseChecksumForFile(checksumData, archiveName)
	if err != nil {
		return nil, fmt.Errorf("parsing checksum: %w", err)
	}

	// Download archive to a temporary file.
	archiveFile, err := os.CreateTemp("", "teanode-update-*.archive")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	archivePath := archiveFile.Name()
	defer func() {
		_ = archiveFile.Close()
		_ = os.Remove(archivePath)
	}()

	if err := downloadToFile(ctx, archiveAsset.BrowserDownloadURL, archiveFile, maxArchiveSize); err != nil {
		return nil, fmt.Errorf("downloading archive: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return nil, fmt.Errorf("closing archive: %w", err)
	}

	// Verify checksum.
	actualChecksum, err := sha256File(archivePath)
	if err != nil {
		return nil, fmt.Errorf("computing checksum: %w", err)
	}
	if actualChecksum != expectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	// Extract binary to a staging directory.
	stageDirectory, err := os.MkdirTemp("", "teanode-stage-*")
	if err != nil {
		return nil, fmt.Errorf("creating staging directory: %w", err)
	}

	targetBinaryName := binaryName
	if runtime.GOOS == "windows" {
		targetBinaryName += ".exe"
	}
	stagedPath := filepath.Join(stageDirectory, targetBinaryName)

	if runtime.GOOS == "windows" {
		err = extractFromZip(archivePath, targetBinaryName, stagedPath)
	} else {
		err = extractFromTarGz(archivePath, targetBinaryName, stagedPath)
	}
	if err != nil {
		_ = os.RemoveAll(stageDirectory)
		return nil, fmt.Errorf("extracting binary: %w", err)
	}

	// Make binary executable on Unix.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(stagedPath, 0755); err != nil {
			_ = os.RemoveAll(stageDirectory)
			return nil, fmt.Errorf("setting permissions: %w", err)
		}
	}

	return &DownloadResult{
		StagedPath: stagedPath,
		Checksum:   actualChecksum,
	}, nil
}

// downloadToMemory downloads a URL into memory with a size limit.
func downloadToMemory(ctx context.Context, url string, maxSize int64) ([]byte, error) {
	requestContext, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", version.ServerName())

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("response exceeds %d bytes", maxSize)
	}
	return data, nil
}

// downloadToFile downloads a URL to an open file with a size limit.
func downloadToFile(ctx context.Context, url string, file *os.File, maxSize int64) error {
	requestContext, cancel := context.WithTimeout(ctx, 10*60*time.Second) // 10-minute download timeout
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", version.ServerName())

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}

	written, err := io.Copy(file, io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return err
	}
	if written > maxSize {
		return fmt.Errorf("response exceeds %d bytes", maxSize)
	}
	return nil
}

// sha256File computes the hex-encoded SHA256 hash of a file.
func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// parseChecksumForFile extracts the SHA256 hex digest for a given file name
// from a SHA256SUMS-formatted checksum file.
func parseChecksumForFile(data []byte, fileName string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: "<hex>  <filename>" or "<hex> <filename>"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := parts[len(parts)-1]
			checksum := parts[0]
			if name == fileName {
				return strings.ToLower(checksum), nil
			}
		}
	}
	return "", fmt.Errorf("no checksum found for %s", fileName)
}

// extractFromTarGz extracts a named file from a .tar.gz archive.
func extractFromTarGz(archivePath, targetName, destinationPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Match either "teanode" or "*/teanode" (in case the archive has a directory prefix).
		baseName := filepath.Base(header.Name)
		if baseName == targetName && header.Typeflag == tar.TypeReg {
			output, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer func() { _ = output.Close() }()

			if _, err := io.Copy(output, io.LimitReader(tarReader, maxArchiveSize)); err != nil {
				return err
			}
			return output.Close()
		}
	}
	return fmt.Errorf("binary %q not found in archive", targetName)
}

// extractFromZip extracts a named file from a .zip archive.
func extractFromZip(archivePath, targetName, destinationPath string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	for _, entry := range reader.File {
		baseName := filepath.Base(entry.Name)
		if baseName == targetName && !entry.FileInfo().IsDir() {
			source, err := entry.Open()
			if err != nil {
				return err
			}
			defer func() { _ = source.Close() }()

			output, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return err
			}
			defer func() { _ = output.Close() }()

			if _, err := io.Copy(output, io.LimitReader(source, maxArchiveSize)); err != nil {
				return err
			}
			return output.Close()
		}
	}
	return fmt.Errorf("binary %q not found in archive", targetName)
}
