package v1api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

// handleAuthStatus returns the current auth state for the frontend.
func (self *API) handleAuthStatus(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodGet {
		return web.ErrMethodNotAllowed
	}

	securityConfig := self.gateway.SecurityConfig()
	passwordSet := securityConfig.Password != ""

	authenticated := false
	if passwordSet {
		// Check if the request has a valid session cookie.
		cookie, err := request.Cookie("session")
		if err == nil && cookie.Value != "" {
			session := self.gateway.SessionStore().Get(cookie.Value)
			if session != nil {
				authenticated = true
			}
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"passwordSet":   passwordSet,
		"authenticated": authenticated,
	})
	return nil
}

// handleAuthSetup handles first-time password setup (onboarding).
func (self *API) handleAuthSetup(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.ErrMethodNotAllowed
	}

	securityConfig := self.gateway.SecurityConfig()
	if securityConfig.Password != "" {
		return web.Error(409, "password already set")
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return web.Error(400, "invalid request body")
	}
	if len(body.Password) < 8 {
		return web.Error(400, "password must be at least 8 characters")
	}

	hash, err := security.HashPassword(body.Password)
	if err != nil {
		return web.Error(500, "failed to hash password")
	}

	// Update in-memory and save to security.yaml.
	securityConfig.Password = string(hash)
	if err := configs.SaveSecurity(securityConfig); err != nil {
		return web.Error(500, "failed to save security config")
	}

	// Auto-create a session for the user.
	maxAge := resolveMaxAge(self.gateway.Config())
	session, err := self.gateway.SessionStore().Create(
		request.UserAgent(),
		request.RemoteAddr,
		maxAge,
	)
	if err != nil {
		return web.Error(500, "failed to create session")
	}

	setSessionCookie(writer, session.ID, maxAge)
	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"ok": true,
	})
	return nil
}

// handleAuthLogin handles password login.
func (self *API) handleAuthLogin(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.ErrMethodNotAllowed
	}

	securityConfig := self.gateway.SecurityConfig()
	if securityConfig.Password == "" {
		return web.Error(400, "no password configured")
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return web.Error(400, "invalid request body")
	}

	if match, err := security.VerifyPassword([]byte(securityConfig.Password), body.Password); err != nil || !match {
		return web.Error(401, "invalid password")
	}

	maxAge := resolveMaxAge(self.gateway.Config())
	session, err := self.gateway.SessionStore().Create(
		request.UserAgent(),
		request.RemoteAddr,
		maxAge,
	)
	if err != nil {
		return web.Error(500, "failed to create session")
	}

	setSessionCookie(writer, session.ID, maxAge)
	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"ok": true,
	})
	return nil
}

// handleAuthLogout deletes the session and clears the cookie.
func (self *API) handleAuthLogout(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.ErrMethodNotAllowed
	}

	cookie, err := request.Cookie("session")
	if err == nil && cookie.Value != "" {
		self.gateway.SessionStore().Delete(cookie.Value)
	}

	// Clear cookie.
	http.SetCookie(writer, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"ok": true,
	})
	return nil
}

func setSessionCookie(writer http.ResponseWriter, sessionId string, maxAge time.Duration) {
	http.SetCookie(writer, &http.Cookie{
		Name:     "session",
		Value:    sessionId,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge.Seconds()),
	})
}

func resolveMaxAge(config *configs.Config) time.Duration {
	if config.Gateway.Auth != nil && config.Gateway.Auth.SessionMaxAgeDays > 0 {
		return time.Duration(config.Gateway.Auth.SessionMaxAgeDays) * 24 * time.Hour
	}
	return 14 * 24 * time.Hour
}
