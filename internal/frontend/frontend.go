// Package frontend serves the embedded SPA static files with history-API fallback.
package frontend

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

//go:embed static
var staticFiles embed.FS

// bundleMetadata holds the parsed contents of bundle.metadata.json.
type bundleMetadata struct {
	BuildID string `json:"buildId"`
}

// cachedBuildId is resolved once at init from the embedded bundle.metadata.json.
var cachedBuildId string

func init() {
	data, err := staticFiles.ReadFile("static/bundle.metadata.json")
	if err != nil {
		return // dev builds may not include bundle.metadata.json
	}
	var meta bundleMetadata
	if err := json.Unmarshal(data, &meta); err == nil {
		cachedBuildId = meta.BuildID
	}
}

// BuildID returns the frontend build fingerprint, or "" if unavailable.
func BuildID() string {
	return cachedBuildId
}

// frontendComponent serves the embedded SPA frontend.
type frontendComponent struct{}

// New returns a web.Component that serves the embedded static files
// with SPA history-API fallback.
func New() *frontendComponent {
	return &frontendComponent{}
}

func (self *frontendComponent) AddRoutes(router *mux.Router) error {
	staticSub, _ := fs.Sub(staticFiles, "static")
	router.PathPrefix("/").Handler(frontendHandler(http.FS(staticSub)))
	return nil
}

// frontendHandler serves static files from the given filesystem, falling back to
// index.html for any path that doesn't match a real file. This supports
// client-side (history API) routing.
func frontendHandler(fileSystem http.FileSystem) http.Handler {
	fileServer := http.FileServer(fileSystem)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := request.URL.Path

		// Determine whether we're serving index.html (directly or as SPA fallback).
		servingIndex := path == "/" || path == "/index.html"
		if !servingIndex {
			lookupPath := strings.TrimPrefix(path, "/")
			if file, err := fileSystem.Open(lookupPath); err != nil {
				request.URL.Path = "/"
				servingIndex = true
			} else {
				_ = file.Close()
			}
		}

		// Enable cross-origin isolation so SharedArrayBuffer is available.
		// Required by ONNX Runtime's WASM backend (used for voice call VAD).
		// Safe because all resources are same-origin.
		writer.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		writer.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")

		if servingIndex {
			writer.Header().Set("Cache-Control", "no-cache")
		} else {
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(writer, request)
	})
}
