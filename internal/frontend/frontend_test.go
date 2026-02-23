package frontend

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	fileSystem, err := fs.Sub(
		fstest.MapFS{
			"static/index.html":      &fstest.MapFile{Data: []byte("<html>index</html>")},
			"static/bundle.test.js":  &fstest.MapFile{Data: []byte("console.log('ok');")},
			"static/bundle.test.css": &fstest.MapFile{Data: []byte("body{}")},
		},
		"static",
	)
	if err != nil {
		t.Fatalf("fs.Sub(): %v", err)
	}
	return frontendHandler(http.FS(fileSystem))
}

func TestFrontendHandler_ServesAssetWithoutFallback(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/bundle.test.js", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") {
		t.Fatalf("content-type = %q, expected javascript", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "console.log('ok')") {
		t.Fatalf("body = %q, expected JS asset content", body)
	}
}

func TestFrontendHandler_FallbacksToIndexForSPARoute(t *testing.T) {
	handler := testHandler(t)

	request := httptest.NewRequest(http.MethodGet, "/conversations/main", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("content-type = %q, expected text/html", contentType)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "<html>index</html>") {
		t.Fatalf("body = %q, expected index fallback content", body)
	}
}
