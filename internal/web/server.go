package web

import (
	"net/http"

	"github.com/gorilla/mux"
)

// Server is an HTTP handler backed by a gorilla/mux router.
type Server interface {
	http.Handler
}

type server struct {
	settings *Settings
	router   *mux.Router
}

// NewServer creates a Server that dispatches to the given components' routes.
func NewServer(settings *Settings, components ...Component) (Server, error) {
	router := mux.NewRouter().StrictSlash(true)
	for _, component := range components {
		if err := component.AddRoutes(router); err != nil {
			return nil, err
		}
	}
	return &server{
		settings: settings,
		router:   router,
	}, nil
}

func (self *server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.router.ServeHTTP(writer, request)
}
