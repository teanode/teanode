package deferutil

import (
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("deferutil")

func Run(deferFunction func() error, err *error) {
	deferError := deferFunction()
	if deferError != nil {
		_, filename, lineNumber, _ := runtime.Caller(2) // get the caller of this function
		if err != nil {
			log.Errorf("failed to run defer statement, called from [%s:%d]: %s", filepath.Base(filename), lineNumber, deferError)
		} else {
			log.Warningf("failed to run defer statement, called from [%s:%d]: %s", filepath.Base(filename), lineNumber, deferError)
		}
	}
	if err != nil && *err == nil {
		*err = deferError
	}
}

// Recover calls recover and logs the stack trace.
func Recover() {
	//nolint:revive
	if err := recover(); err != nil {
		log.Criticalf("recovered from panic: %+v\n%s", err, debug.Stack())
	}
}
