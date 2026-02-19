// Package frontend serves the embedded SPA static files with history-API fallback.
package frontend

import (
	"net/http"
)

// frontendComponent serves the embedded SPA frontend.
type frontendComponent struct{}

// New returns a web.Component that serves the embedded static files
// with SPA history-API fallback.
func New() *frontendComponent {
	return &frontendComponent{}
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
		if file, err := fileSystem.Open(path); err != nil {
			request.URL.Path = "/"
			servingIndex = true
		} else {
			file.Close()
		}

		if servingIndex {
			writer.Header().Set("Cache-Control", "no-cache")
		} else {
			writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(writer, request)
	})
}
