package web

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/handlers"

	"github.com/teanode/teanode/internal/util/bufferpool"
)

// Middleware wraps an http.Handler to add cross-cutting behaviour.
type Middleware func(http.Handler) http.Handler

// ApplyMiddlewares wraps handler with each middleware in order.
func ApplyMiddlewares(handler http.Handler, middlewares ...Middleware) http.Handler {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}
	return handler
}

type accessLog struct {
	Timestamp  time.Time `json:"timestamp,omitempty"`
	IP         string    `json:"ip,omitempty"`
	Scheme     string    `json:"scheme,omitempty"`
	Host       string    `json:"host,omitempty"`
	User       string    `json:"user,omitempty"`
	Method     string    `json:"method,omitempty"`
	URI        string    `json:"uri,omitempty"`
	Protocol   string    `json:"protocol,omitempty"`
	StatusCode int       `json:"statusCode,omitempty"`
	Size       int       `json:"size"`
	Referer    string    `json:"referer,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	Elapsed    float64   `json:"elapsed,omitempty"`
}

// LoggingMiddleware writes structured JSON access logs to stdout.
func LoggingMiddleware(handler http.Handler) http.Handler {
	timestampFormat := "2006-01-02T15:04:05.000000-07:00,"
	return handlers.CustomLoggingHandler(os.Stdout, handler, func(writer io.Writer, params handlers.LogFormatterParams) {
		scheme := "http"
		if params.Request.TLS != nil {
			scheme = "https"
		}

		user := ""
		if params.URL.User != nil {
			user = params.URL.User.Username()
		}

		buffer, releaseBuffer := bufferpool.AcquireBuffer()
		defer releaseBuffer()

		if _, err := buffer.WriteString(timestampFormat); err != nil {
			log.Errorf("failed to write timestamp for access log: %s", err)
			return
		}

		if err := json.NewEncoder(buffer).Encode(&accessLog{
			Timestamp:  params.TimeStamp,
			IP:         params.Request.RemoteAddr,
			Scheme:     scheme,
			Host:       params.Request.Host,
			User:       user,
			Method:     params.Request.Method,
			URI:        params.Request.RequestURI,
			Protocol:   params.Request.Proto,
			StatusCode: params.StatusCode,
			Size:       params.Size,
			Referer:    params.Request.Referer(),
			UserAgent:  params.Request.UserAgent(),
			Elapsed:    time.Since(params.TimeStamp).Seconds(),
		}); err != nil {
			log.Errorf("failed to encode access log: %s", err)
			return
		}

		raw := buffer.Bytes()
		copy(raw, []byte(time.Now().Format(timestampFormat)))

		if _, err := writer.Write(raw); err != nil {
			log.Errorf("failed to write access log: %s", err)
			return
		}
	})
}

// CompressionMiddleware applies gzip/deflate compression to responses.
func CompressionMiddleware(handler http.Handler) http.Handler {
	return handlers.CompressHandler(handler)
}

// MakeServerNameMiddleware returns a middleware that sets the Server response header.
func MakeServerNameMiddleware(serverName string) Middleware {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Server", serverName)
			handler.ServeHTTP(writer, request)
		})
	}
}

// MakeForwarderMiddleware returns a middleware that trusts X-Forwarded-For only
// when accompanied by the correct X-Forwarder-Key.
func MakeForwarderMiddleware(forwarderKey string) Middleware {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if ip, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
				request.RemoteAddr = ip
			}
			if forwardedFor := request.Header.Get("X-Forwarded-For"); forwardedFor != "" {
				if forwarderKey != "" && request.Header.Get("X-Forwarder-Key") != forwarderKey {
					log.Warningf("request from %s has X-Forwarded-For header %q, but has invalid X-Forwarder-Key", request.RemoteAddr, forwardedFor)
					WriteError(writer, ErrServiceUnavailable)
					return
				}
				ips := strings.Split(forwardedFor, ",")
				request.RemoteAddr = strings.TrimSpace(ips[0])
			}
			delete(request.Header, "X-Forwarder-Key")
			request.Header.Set("X-Forwarded-For", request.RemoteAddr)
			handler.ServeHTTP(writer, request)
		})
	}
}
