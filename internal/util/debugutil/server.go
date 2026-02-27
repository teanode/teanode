// Package debugutil exposes a standalone debug HTTP server with pprof and runtime diagnostics.
package debugutil

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/teanode/teanode/internal/util/deferutil"
)

func RunDebugServer(context context.Context, endpoint string) (func(), error) {
	log.Debugf("listening on debug endpoint: %s", endpoint)
	var listenerConfiguration net.ListenConfig
	debugListener, listenError := listenerConfiguration.Listen(context, "tcp", endpoint)
	if listenError != nil {
		log.Errorf("failed to listen on %q: %s", endpoint, listenError)
		return nil, listenError
	}
	debugHandler := http.NewServeMux()
	debugHandler.HandleFunc("/debug/pprof/", pprof.Index)
	debugHandler.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	debugHandler.HandleFunc("/debug/pprof/profile", pprof.Profile)
	debugHandler.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	debugHandler.HandleFunc("/debug/pprof/trace", pprof.Trace)
	debugHandler.HandleFunc("/debug/FreeOSMemory", func(response http.ResponseWriter, request *http.Request) {
		start := time.Now()
		debug.FreeOSMemory()
		log.Infof("called debug.FreeOSMemory() from %q, took %s", request.RemoteAddr, time.Since(start))
	})
	debugHandler.HandleFunc("/debug/GC", func(response http.ResponseWriter, request *http.Request) {
		start := time.Now()
		runtime.GC()
		log.Infof("called runtime.GC() from %q, took %s", request.RemoteAddr, time.Since(start))
	})
	debugHandler.HandleFunc("/debug/ReadMemStats", func(response http.ResponseWriter, request *http.Request) {
		start := time.Now()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		data, marshalError := json.Marshal(stats)
		if marshalError != nil {
			http.Error(response, marshalError.Error(), http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(data)
		log.Infof("called runtime.ReadMemStats() from %q, took %s", request.RemoteAddr, time.Since(start))
	})
	debugServer := &http.Server{
		Handler: debugHandler,
	}
	go func() {
		defer deferutil.Recover()

		log.Debugf("running and serving debug endpoint")
		if serveError := debugServer.Serve(debugListener); serveError != nil && !errors.Is(serveError, http.ErrServerClosed) {
			log.Errorf("debug server exited with error: %s", serveError)
		}
		log.Debugf("stop serving debug endpoint")
	}()
	return func() {
		if shutdownError := debugServer.Shutdown(context); shutdownError != nil {
			log.Errorf("failed to shutdown debug server: %s", shutdownError)
		}
	}, nil
}
