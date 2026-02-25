package v1api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/onboarding"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/ratelimit"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/web"
)

// handleAuthStatus returns the current auth state for the frontend.
func (self *v1Api) handleAuthStatus(writer http.ResponseWriter, request *http.Request) error {
	if request.Method != http.MethodGet {
		return web.ErrMethodNotAllowed
	}

	passwordSet := false
	if transactionError := store.StoreFromContext(request.Context()).Transaction(func(transaction store.Transaction) error {
		users, listError := transaction.ListUsers(nil)
		if listError != nil {
			return listError
		}
		for _, user := range users {
			if strings.TrimSpace(valueOrEmpty(user.Password)) != "" {
				passwordSet = true
				break
			}
		}
		return nil
	}); transactionError != nil {
		return web.Error(500, "failed to load users")
	}

	authenticated := false
	isAdmin := false
	if passwordSet {
		cookie, err := request.Cookie("session")
		if err == nil && cookie.Value != "" {
			session, found := self.getSessionByID(request.Context(), cookie.Value)
			if found && strings.TrimSpace(valueOrEmpty(session.UserID)) != "" {
				authenticated = true
				_ = store.StoreFromContext(request.Context()).Transaction(func(transaction store.Transaction) error {
					user, getUserError := transaction.GetUser(strings.TrimSpace(valueOrEmpty(session.UserID)), nil)
					if getUserError != nil {
						return nil
					}
					isAdmin = user.Admin != nil && *user.Admin
					return nil
				})
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

	passwordSet := false
	if transactionError := store.StoreFromContext(request.Context()).Transaction(func(transaction store.Transaction) error {
		users, usersError := transaction.ListUsers(nil)
		if usersError != nil {
			return usersError
		}
		for _, user := range users {
			if strings.TrimSpace(valueOrEmpty(user.Password)) != "" {
				passwordSet = true
				break
			}
		}
		return nil
	}); transactionError != nil {
		return web.Error(500, "failed to load users")
	}
	if passwordSet {
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
	admin := true
	createdUserID := ""
	if err := store.StoreFromContext(request.Context()).Transaction(func(transaction store.Transaction) error {
		if _, _, found := transaction.GetUserByUsername(username, nil); found {
			return web.Error(409, "username already exists")
		}
		user := &models.User{
			ID:       security.NewULID(),
			Username: &username,
			Password: ptrto.TrimmedString(string(hash)),
			Admin:    &admin,
		}
		createdUser, createUserError := transaction.CreateUser(user, nil, nil)
		if createUserError != nil {
			return createUserError
		}
		createdUserID = createdUser.ID
		return nil
	}); err != nil {
		if typedError, ok := err.(*web.HTTPError); ok {
			return typedError
		}
		return web.Error(500, "failed to create user")
	}

	if err := onboarding.InitializeUser(request.Context(), self.gateway, createdUserID); err != nil {
		return web.Error(500, "failed to initialize user onboarding")
	}

	maxAge := resolveMaxAge(self.gateway.Config())
	sessionID, err := self.createSession(
		request.Context(),
		createdUserID,
		request.UserAgent(),
		request.RemoteAddr,
		maxAge,
	)
	if err != nil {
		return web.Error(500, "failed to create session")
	}

	setSessionCookie(writer, sessionID, maxAge)
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

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return web.Error(400, "invalid request body")
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		return web.Error(400, "username is required")
	}

	userID := ""
	var user *models.User
	if err := store.StoreFromContext(request.Context()).Transaction(func(transaction store.Transaction) error {
		foundUserID, foundUser, found := transaction.GetUserByUsername(username, nil)
		if !found {
			return nil
		}
		userID = foundUserID
		user = foundUser
		return nil
	}); err != nil {
		return web.Error(500, "failed to load users")
	}

	if user == nil {
		return web.Error(401, "invalid credentials")
	}
	if match, err := security.VerifyPassword([]byte(valueOrEmpty(user.Password)), body.Password); err != nil || !match {
		return web.Error(401, "invalid credentials")
	}

	maxAge := resolveMaxAge(self.gateway.Config())
	sessionID, err := self.createSession(
		request.Context(),
		userID,
		request.UserAgent(),
		request.RemoteAddr,
		maxAge,
	)
	if err != nil {
		return web.Error(500, "failed to create session")
	}

	setSessionCookie(writer, sessionID, maxAge)
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
		_ = self.deleteSession(request.Context(), cookie.Value)
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

func setSessionCookie(writer http.ResponseWriter, sessionID string, maxAge time.Duration) {
	http.SetCookie(writer, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge.Seconds()),
	})
}

func (self *v1Api) getSessionByID(ctx context.Context, sessionID string) (*models.Session, bool) {
	var session *models.Session
	if err := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		existingSession, getError := transaction.GetSession(sessionID, nil)
		if getError != nil {
			return nil
		}
		if existingSession.ExpiresAt != nil && time.Now().After(*existingSession.ExpiresAt) {
			_ = transaction.DeleteSession(sessionID, nil)
			return nil
		}
		session = existingSession
		return nil
	}); err != nil || session == nil {
		return nil, false
	}
	return session, true
}

func (self *v1Api) createSession(ctx context.Context, userID string, userAgent string, remoteAddress string, maxAge time.Duration) (string, error) {
	sessionID := security.NewULID()
	now := time.Now()
	expiresAt := now.Add(maxAge)
	if err := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		_, createError := transaction.CreateSession(&models.Session{
			ID:            sessionID,
			UserID:        ptrto.TrimmedString(userID),
			UserAgent:     ptrto.Value(userAgent),
			RemoteAddress: ptrto.Value(remoteAddress),
			ExpiresAt:     &expiresAt,
			CreatedAt:     &now,
			ModifiedAt:    &now,
		}, nil)
		return createError
	}); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (self *v1Api) deleteSession(ctx context.Context, sessionID string) error {
	return store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		return transaction.DeleteSession(sessionID, nil)
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

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
