package gw

import (
	"net/http"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/web"
)

// resolveSessionMaxAge returns the session max age from config, defaulting to 14 days.
func (self *gateway) resolveSessionMaxAge() time.Duration {
	if self.config.Gateway.Auth != nil && self.config.Gateway.Auth.SessionMaxAgeDays > 0 {
		return time.Duration(self.config.Gateway.Auth.SessionMaxAgeDays) * 24 * time.Hour
	}
	return 14 * 24 * time.Hour
}

// checkBearerToken validates bearer auth and injects user context when valid.
func (self *gateway) checkBearerToken(request *http.Request) (*http.Request, bool) {
	if self.securityConfig == nil {
		return request, false
	}
	authHeader := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return request, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if token == "" {
		return request, false
	}
	userId, _, _, found := self.securityConfig.FindUserByToken(token)
	if !found || strings.TrimSpace(userId) == "" {
		return request, false
	}
	user := &UserContext{
		UserID:     userId,
		AuthMethod: AuthMethodToken,
	}
	return request.WithContext(ContextWithUser(request.Context(), user)), true
}

// checkSessionCookie validates session auth and injects user context when valid.
func (self *gateway) checkSessionCookie(request *http.Request) (*http.Request, bool) {
	if self.sessionStore == nil {
		return request, false
	}
	cookie, err := request.Cookie("session")
	if err != nil || cookie.Value == "" {
		return request, false
	}
	session := self.sessionStore.Get(cookie.Value)
	if session == nil || strings.TrimSpace(session.UserID) == "" {
		return request, false
	}
	// Renew session asynchronously (throttled to once per hour inside Touch).
	go self.sessionStore.Touch(session.ID, self.resolveSessionMaxAge())

	user := &UserContext{
		UserID:     session.UserID,
		SessionID:  session.ID,
		AuthMethod: AuthMethodSession,
	}
	return request.WithContext(ContextWithUser(request.Context(), user)), true
}

// AuthMiddleware returns a web.Middleware that enforces token/session auth.
func (self *gateway) AuthMiddleware() web.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			path := request.URL.Path

			// 1. Non-/api/ paths (frontend static files): always allow.
			if !strings.HasPrefix(path, "/api/") {
				next.ServeHTTP(writer, request)
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

			// 4b. Media upload: requires session or bearer auth.
			if path == "/api/v1/media/upload" {
				if authorizedRequest, authorized := self.checkSessionCookie(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				if authorizedRequest, authorized := self.checkBearerToken(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				web.WriteError(writer, web.ErrUnauthorized)
				return
			}

			// 4c. Audio endpoints: requires session or bearer auth.
			if strings.HasPrefix(path, "/api/v1/audio/") {
				if authorizedRequest, authorized := self.checkSessionCookie(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				if authorizedRequest, authorized := self.checkBearerToken(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				web.WriteError(writer, web.ErrUnauthorized)
				return
			}

			// 5. Machine endpoints: token-only auth.
			if path == "/api/v1/browser" || path == "/api/v1/terminal" || path == "/api/v1/chat/completions" {
				if authorizedRequest, authorized := self.checkBearerToken(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				web.WriteError(writer, web.ErrUnauthorized)
				return
			}

			// 6. Websocket api: accept session cookie or bearer token.
			if path == "/api/v1/websocket" {
				if authorizedRequest, authorized := self.checkSessionCookie(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				if authorizedRequest, authorized := self.checkBearerToken(request); authorized {
					next.ServeHTTP(writer, authorizedRequest)
					return
				}
				web.WriteError(writer, web.ErrUnauthorized)
				return
			}

			web.WriteError(writer, web.ErrUnauthorized)
		})
	}
}
