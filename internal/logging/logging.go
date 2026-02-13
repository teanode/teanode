package logging

import (
	"os"

	"github.com/op/go-logging"
)

var format = logging.MustStringFormatter(
	"%{color}%{time:2006-01-02 15:04:05.000000} %{module} [%{level}] <%{pid}> [%{shortfile} %{shortfunc}] %{message}%{color:reset}",
)

// Setup initializes the root logging backend with the given level.
// It should be called once at program startup.
func Setup(level logging.Level) {
	backend := logging.NewLogBackend(os.Stderr, "", 0)
	formatted := logging.NewBackendFormatter(backend, format)
	leveled := logging.AddModuleLevel(formatted)
	leveled.SetLevel(level, "")
	logging.SetBackend(leveled)
}

// Get returns a named logger for the given module.
func Get(module string) *logging.Logger {
	return logging.MustGetLogger(module)
}
