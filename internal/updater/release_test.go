package updater

import "testing"

func TestAssetName(t *testing.T) {
	tests := []struct {
		version      string
		os           string
		arch         string
		expectedName string
	}{
		{"0.1.4", "linux", "amd64", "teanode_0.1.4_linux_amd64.tar.gz"},
		{"0.1.4", "linux", "arm64", "teanode_0.1.4_linux_arm64.tar.gz"},
		{"0.1.4", "darwin", "amd64", "teanode_0.1.4_darwin_amd64.tar.gz"},
		{"0.1.4", "darwin", "arm64", "teanode_0.1.4_darwin_arm64.tar.gz"},
		{"0.1.4", "windows", "amd64", "teanode_0.1.4_windows_amd64.zip"},
		{"1.0.0", "linux", "amd64", "teanode_1.0.0_linux_amd64.tar.gz"},
	}

	for _, testCase := range tests {
		result := AssetName(testCase.version, testCase.os, testCase.arch)
		if result != testCase.expectedName {
			t.Errorf("AssetName(%q, %q, %q) = %q, want %q",
				testCase.version, testCase.os, testCase.arch, result, testCase.expectedName)
		}
	}
}

func TestChecksumAssetName(t *testing.T) {
	result := ChecksumAssetName("0.1.4")
	expected := "teanode_0.1.4_SHA256SUMS"
	if result != expected {
		t.Errorf("ChecksumAssetName(%q) = %q, want %q", "0.1.4", result, expected)
	}
}

func TestReleaseInfoFindAsset(t *testing.T) {
	release := &ReleaseInfo{
		Assets: []ReleaseAsset{
			{Name: "teanode_0.1.4_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/a"},
			{Name: "teanode_0.1.4_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/b"},
			{Name: "teanode_0.1.4_SHA256SUMS", BrowserDownloadURL: "https://example.com/c"},
		},
	}

	asset := release.FindAsset("teanode_0.1.4_linux_amd64.tar.gz")
	if asset == nil {
		t.Fatal("FindAsset returned nil for existing asset")
	}
	if asset.BrowserDownloadURL != "https://example.com/a" {
		t.Errorf("FindAsset URL = %q, want %q", asset.BrowserDownloadURL, "https://example.com/a")
	}

	missing := release.FindAsset("teanode_0.1.4_windows_amd64.zip")
	if missing != nil {
		t.Errorf("FindAsset returned non-nil for missing asset: %v", missing)
	}
}

func TestReleaseInfoVersion(t *testing.T) {
	release := &ReleaseInfo{TagName: "v0.1.4"}
	if got := release.Version(); got != "0.1.4" {
		t.Errorf("Version() = %q, want %q", got, "0.1.4")
	}

	release2 := &ReleaseInfo{TagName: "0.1.4"}
	if got := release2.Version(); got != "0.1.4" {
		t.Errorf("Version() = %q, want %q", got, "0.1.4")
	}
}
