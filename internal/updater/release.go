// Package updater implements self-update checking, downloading, verification,
// and application for the TeaNode binary via GitHub Releases.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/version"
)

var log = logging.MustGetLogger("updater")

const (
	// gitHubOwner is the GitHub repository owner.
	gitHubOwner = "teanode"
	// gitHubRepo is the GitHub repository name.
	gitHubRepo = "teanode"
	// latestReleaseUrl is the GitHub API endpoint for the latest release.
	latestReleaseUrl = "https://api.github.com/repos/" + gitHubOwner + "/" + gitHubRepo + "/releases/latest"
	// httpTimeout is the timeout for HTTP requests.
	httpTimeout = 30 * time.Second
)

// ReleaseInfo holds metadata about a GitHub release.
type ReleaseInfo struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	PublishedAt time.Time      `json:"published_at"`
	HTMLURL     string         `json:"html_url"`
	Assets      []ReleaseAsset `json:"assets"`
}

// ReleaseAsset holds metadata about a single release asset.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

// Version returns the semantic version string without the leading "v".
func (self *ReleaseInfo) Version() string {
	return strings.TrimPrefix(self.TagName, "v")
}

// AssetName returns the expected archive file name for the given OS and architecture.
func AssetName(releaseVersion, operatingSystem, architecture string) string {
	extension := "tar.gz"
	if operatingSystem == "windows" {
		extension = "zip"
	}
	return fmt.Sprintf("teanode_%s_%s_%s.%s", releaseVersion, operatingSystem, architecture, extension)
}

// ChecksumAssetName returns the expected checksum file name for the given version.
func ChecksumAssetName(releaseVersion string) string {
	return fmt.Sprintf("teanode_%s_SHA256SUMS", releaseVersion)
}

// FindAsset returns the release asset matching the expected name, or nil.
func (self *ReleaseInfo) FindAsset(name string) *ReleaseAsset {
	for index := range self.Assets {
		if self.Assets[index].Name == name {
			return &self.Assets[index]
		}
	}
	return nil
}

// CheckLatestRelease fetches the latest release info from GitHub.
func CheckLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	requestContext, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, latestReleaseUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("updater: creating request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", version.ServerName())

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("updater: fetching latest release: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return nil, fmt.Errorf("updater: GitHub API returned %d: %s", response.StatusCode, string(body))
	}

	var release ReleaseInfo
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("updater: decoding release: %w", err)
	}
	return &release, nil
}

// PlatformAssetName returns the expected archive name for the current OS/arch.
func PlatformAssetName(releaseVersion string) string {
	return AssetName(releaseVersion, runtime.GOOS, runtime.GOARCH)
}
