package web

import (
	"github.com/gorilla/mux"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("web")

// Settings holds web server configuration.
type Settings struct {
}

// Component is a module that registers HTTP routes on the server's router.
type Component interface {
	AddRoutes(*mux.Router) error
}
