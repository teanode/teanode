//go:build !test

package frontend

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
)

//go:embed static
var staticFiles embed.FS

func (self *frontendComponent) AddRoutes(router *mux.Router) error {
	staticSub, _ := fs.Sub(staticFiles, "static")
	router.PathPrefix("/").Handler(frontendHandler(http.FS(staticSub)))
	return nil
}
