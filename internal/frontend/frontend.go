// Package frontend serves the embedded SPA static files with history-API fallback.
package frontend

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
)

//go:embed static
var staticFiles embed.FS

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
		// Try opening the requested file.
		file, err := fileSystem.Open(path)
		if err != nil {
			// File doesn't exist — serve index.html for SPA routing.
			request.URL.Path = "/"
			fileServer.ServeHTTP(writer, request)
			return
		}
		file.Close()
		fileServer.ServeHTTP(writer, request)
	})
}
