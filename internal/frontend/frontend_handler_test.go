//go:build test

package frontend

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendHandlerCachingAndFallback(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "app.js"), []byte("APP"), 0o644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	handler := frontendHandler(http.Dir(tempDir))

	t.Run("index path is no-cache", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("cache-control = %q, want %q", got, "no-cache")
		}
	})

	t.Run("asset path is immutable", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/app.js", nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("cache-control = %q, want immutable policy", got)
		}
		if !strings.Contains(recorder.Body.String(), "APP") {
			t.Fatalf("expected app.js content in response, got %q", recorder.Body.String())
		}
	})

	t.Run("missing path falls back to index", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("cache-control = %q, want %q", got, "no-cache")
		}
		if !strings.Contains(recorder.Body.String(), "INDEX") {
			t.Fatalf("expected index fallback, got %q", recorder.Body.String())
		}
	})
}
