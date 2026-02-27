package web

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/handlers"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
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
				request.RemoteAddr = ips[0]
			}
			delete(request.Header, "X-Forwarder-Key")
			request.Header.Set("X-Forwarded-For", request.RemoteAddr)
			handler.ServeHTTP(writer, request)
		})
	}
}

// AuthenticationMiddleware returns a middleware that enforces token/session auth
// on API endpoints. It reads the store from the request context.
func MakeAuthenticationMiddleware() Middleware {
	// checkToken validates bearer auth (header or query param) and injects user context when valid.
	checkToken := func(request *http.Request) (*http.Request, bool) {
		tokenValue := ""
		if authHeader := request.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			tokenValue = strings.TrimPrefix(authHeader, "Bearer ")
		}
		if tokenValue == "" {
			tokenValue = request.URL.Query().Get("token")
		}
		if tokenValue == "" {
			return request, false
		}
		var user *models.User
		var token *models.Token
		if err := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
			existingToken, err := transaction.GetTokenByToken(ctx, tokenValue, nil)
			if err != nil {
				return err
			}
			existingUser, err := transaction.GetUser(ctx, existingToken.GetUserID(), nil)
			if err != nil {
				return err
			}
			user = existingUser
			token = existingToken
			return nil
		}); err != nil {
			return request, false
		}
		if user == nil || user.ID == "" {
			return request, false
		}
		return request.WithContext(models.ContextWithUserSessionToken(request.Context(), user, nil, token)), true
	}

	// checkSession validates session auth and injects user context when valid.
	checkSession := func(request *http.Request) (*http.Request, bool) {
		cookie, err := request.Cookie("session")
		if err != nil || cookie.Value == "" {
			return request, false
		}
		maxAge := 14 * 24 * time.Hour
		var session *models.Session
		var user *models.User
		if err := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
			existingSession, err := transaction.GetSession(ctx, cookie.Value, nil)
			if err != nil {
				return err
			}
			if existingSession.ExpiresAt != nil && time.Now().After(*existingSession.ExpiresAt) {
				_ = transaction.DeleteSession(ctx, cookie.Value, nil)
				return nil
			}
			if existingSession.UserID == nil || *existingSession.UserID == "" {
				return nil
			}
			existingUser, err := transaction.GetUser(ctx, *existingSession.UserID, nil)
			if err != nil {
				return err
			}
			if existingSession.ModifiedAt != nil && time.Since(*existingSession.ModifiedAt) >= time.Hour {
				updatedSession, err := transaction.ModifySession(ctx, existingSession.ID, func(session *models.Session) error {
					expiresAt := time.Now().Add(maxAge)
					session.ExpiresAt = &expiresAt
					return nil
				}, nil)
				if err != nil {
					return err
				}
				existingSession = updatedSession
			}
			session = existingSession
			user = existingUser
			return nil
		}); err != nil {
			return request, false
		}
		if session == nil || user == nil {
			return request, false
		}
		return request.WithContext(models.ContextWithUserSessionToken(request.Context(), user, session, nil)), true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			path := request.URL.Path

			// 1. Non-/api/ paths (frontend static files): always allow.
			if !strings.HasPrefix(path, "/api/") {
				next.ServeHTTP(writer, request)
				return
			}

			if authorizedRequest, authorized := checkSession(request); authorized {
				next.ServeHTTP(writer, authorizedRequest)
				return
			}
			if authorizedRequest, authorized := checkToken(request); authorized {
				next.ServeHTTP(writer, authorizedRequest)
				return
			}

			// 2. Health endpoint: always allow.
			if path == "/api/v1/health" {
				next.ServeHTTP(writer, request)
				return
			}

			// 3. Auth endpoints: always allow.
			if strings.HasPrefix(path, "/api/v1/auth/") {
				next.ServeHTTP(writer, request)
				return
			}

			// 4. Media GET endpoints: always allow (LLM providers fetch images).
			if strings.HasPrefix(path, "/api/v1/media/") && request.Method == "GET" {
				next.ServeHTTP(writer, request)
				return
			}

			WriteError(writer, ErrUnauthorized)
		})
	}
}
