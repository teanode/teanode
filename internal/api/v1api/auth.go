package v1api

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/onboarding"
	"github.com/teanode/teanode/internal/util/ratelimit"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

// handleAuthStatus returns the current auth state for the frontend.
func (self *v1Api) handleAuthStatus(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodGet {
		return web.ErrMethodNotAllowed
	}

	securityConfig := self.gateway.SecurityConfig()
	passwordSet := securityConfig.HasPasswordConfigured()

	authenticated := false
	isAdmin := false
	if passwordSet {
		// Check if the request has a valid session cookie.
		cookie, err := request.Cookie("session")
		if err == nil && cookie.Value != "" {
			session := self.gateway.SessionStore().Get(cookie.Value)
			if session != nil && session.UserID != "" {
				authenticated = true
				isAdmin = securityConfig.IsAdmin(session.UserID)
			}
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"passwordSet":   passwordSet,
		"authenticated": authenticated,
		"isAdmin":       isAdmin,
	})
	return nil
}

// handleAuthSetup handles first-time password setup (onboarding).
func (self *v1Api) handleAuthSetup(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.ErrMethodNotAllowed
	}

	if err := self.checkAuthRateLimit(request); err != nil {
		return err
	}

	securityConfig := self.gateway.SecurityConfig()
	if securityConfig.HasPasswordConfigured() {
		return web.Error(409, "password already set")
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"name"`
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

	username := strings.TrimSpace(body.Username)
	if username == "" {
		return web.Error(400, "username is required")
	}
	securityConfig.Lock()
	for _, user := range securityConfig.Users {
		if strings.EqualFold(strings.TrimSpace(user.Username), username) {
			securityConfig.Unlock()
			return web.Error(409, "username already exists")
		}
	}
	if securityConfig.Users == nil {
		securityConfig.Users = map[string]configs.SecurityUser{}
	}
	userId := security.NewULID()
	securityConfig.Users[userId] = configs.SecurityUser{
		Username:     username,
		Admin:        true,
		PasswordHash: string(hash),
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = configs.OSUsername()
	}
	profile := &configs.UserProfile{Name: name}
	if err := configs.SaveUserProfile(userId, profile); err != nil {
		securityConfig.Unlock()
		return web.Error(500, "failed to save profile")
	}

	// Update in-memory and save to security.yaml.
	if err := configs.SaveSecurity(securityConfig); err != nil {
		securityConfig.Unlock()
		return web.Error(500, "failed to save security config")
	}
	securityConfig.Unlock()
	if err := onboarding.InitializeUser(self.gateway, userId); err != nil {
		return web.Error(500, "failed to initialize user onboarding")
	}
	// Auto-create a session for the user.
	maxAge := resolveMaxAge(self.gateway.Config())
	session, err := self.gateway.SessionStore().Create(
		userId,
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
func (self *v1Api) handleAuthLogin(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodPost {
		return web.ErrMethodNotAllowed
	}

	if err := self.checkAuthRateLimit(request); err != nil {
		return err
	}

	securityConfig := self.gateway.SecurityConfig()
	if !securityConfig.HasPasswordConfigured() {
		return web.Error(400, "no password configured")
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return web.Error(400, "invalid request body")
	}

	username := strings.TrimSpace(body.Username)
	userId, user, found := securityConfig.FindUserByUsername(username)
	if username == "" {
		return web.Error(400, "username is required")
	}
	if !found {
		return web.Error(401, "invalid credentials")
	}
	if match, err := security.VerifyPassword([]byte(user.PasswordHash), body.Password); err != nil || !match {
		return web.Error(401, "invalid credentials")
	}

	maxAge := resolveMaxAge(self.gateway.Config())
	session, err := self.gateway.SessionStore().Create(
		userId,
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
func (self *v1Api) handleAuthLogout(writer http.ResponseWriter, request *http.Request) error {
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

func authBucketKeyForRemoteAddress(remoteAddress string) string {
	trimmedAddress := strings.TrimSpace(remoteAddress)
	if trimmedAddress == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(trimmedAddress)
	if err == nil {
		trimmedHost := strings.TrimSpace(host)
		if trimmedHost != "" {
			return trimmedHost
		}
	}
	return trimmedAddress
}

// authBucketForRemoteAddress returns the per-IP rate limit bucket for auth endpoints,
// creating one if it doesn't exist. Allows a burst of 5 requests, refilling
// at 1 request per 10 seconds.
func (self *v1Api) authBucketForRemoteAddress(remoteAddress string) *ratelimit.Bucket {
	self.authBucketsMutex.Lock()
	defer self.authBucketsMutex.Unlock()

	const maxAuthBucketEntries = 2048
	const staleEntryDuration = 24 * time.Hour

	now := time.Now()
	if len(self.authBuckets) > maxAuthBucketEntries {
		staleBefore := now.Add(-staleEntryDuration)
		for key, entry := range self.authBuckets {
			if entry.lastSeen.Before(staleBefore) {
				delete(self.authBuckets, key)
			}
		}
	}

	key := authBucketKeyForRemoteAddress(remoteAddress)
	entry, exists := self.authBuckets[key]
	if !exists {
		entry = &authBucketEntry{
			bucket: ratelimit.NewBucketWithQuantumAndInterval(1, 10*time.Second, 5),
		}
		self.authBuckets[key] = entry
	}
	entry.lastSeen = now
	return entry.bucket
}

// checkAuthRateLimit consumes one token from the per-IP auth bucket.
// Returns a 429 error if the rate limit is exceeded.
func (self *v1Api) checkAuthRateLimit(request *http.Request) error {
	bucket := self.authBucketForRemoteAddress(request.RemoteAddr)
	if bucket.TakeAvailable(1) == 0 {
		return web.Error(429, "too many requests")
	}
	return nil
}
