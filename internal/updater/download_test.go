package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestParseChecksumForFile(t *testing.T) {
	checksumData := []byte(`abc123def456  teanode_0.1.4_linux_amd64.tar.gz
789abc012def  teanode_0.1.4_darwin_arm64.tar.gz
DEADBEEF0001  teanode_0.1.4_windows_amd64.zip
`)

	tests := []struct {
		fileName string
		expected string
		wantErr  bool
	}{
		{"teanode_0.1.4_linux_amd64.tar.gz", "abc123def456", false},
		{"teanode_0.1.4_darwin_arm64.tar.gz", "789abc012def", false},
		{"teanode_0.1.4_windows_amd64.zip", "deadbeef0001", false}, // lowercased
		{"teanode_0.1.4_nonexistent.tar.gz", "", true},
	}

	for _, testCase := range tests {
		result, err := parseChecksumForFile(checksumData, testCase.fileName)
		if testCase.wantErr {
			if err == nil {
				t.Errorf("parseChecksumForFile(%q) expected error, got nil", testCase.fileName)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseChecksumForFile(%q) unexpected error: %v", testCase.fileName, err)
			continue
		}
		if result != testCase.expected {
			t.Errorf("parseChecksumForFile(%q) = %q, want %q",
				testCase.fileName, result, testCase.expected)
		}
	}
}

func TestParseChecksumVariousFormats(t *testing.T) {
	// Some sha256sum implementations use two spaces, others use one.
	singleSpace := []byte("abc123 file.tar.gz\n")
	result, err := parseChecksumForFile(singleSpace, "file.tar.gz")
	if err != nil {
		t.Fatalf("single space: unexpected error: %v", err)
	}
	if result != "abc123" {
		t.Errorf("single space: got %q, want %q", result, "abc123")
	}

	doubleSpace := []byte("abc123  file.tar.gz\n")
	result, err = parseChecksumForFile(doubleSpace, "file.tar.gz")
	if err != nil {
		t.Fatalf("double space: unexpected error: %v", err)
	}
	if result != "abc123" {
		t.Errorf("double space: got %q, want %q", result, "abc123")
	}
}

func TestDownloadToMemoryRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("abcdef"))
	}))
	defer server.Close()

	_, err := downloadToMemory(context.Background(), server.URL, 5)
	if err == nil {
		t.Fatal("downloadToMemory expected error for oversized response")
	}
}

func TestDownloadToFileRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("abcdef"))
	}))
	defer server.Close()

	file, err := os.CreateTemp("", "teanode-download-test-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	err = downloadToFile(context.Background(), server.URL, file, 5)
	if err == nil {
		t.Fatal("downloadToFile expected error for oversized response")
	}
}
