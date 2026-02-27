package v1api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

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

	passwordSet := true
	user := models.UserFromContext(request.Context())
	if user == nil {
		if err := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
			users, err := transaction.ListUsers(ctx, nil)
			if err != nil {
				return err
			}
			passwordSet = len(users) > 0
			return nil
		}); err != nil {
			return web.Error(500, "failed to load users")
		}
	}

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"passwordSet":   passwordSet,
		"authenticated": user != nil,
		"isAdmin":       user != nil && user.GetAdmin(),
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
	if transactionError := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		users, usersError := transaction.ListUsers(ctx, nil)
		if usersError != nil {
			return usersError
		}
		for _, user := range users {
			if user.GetPassword() != "" {
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

	username := body.Username
	if username == "" {
		return web.Error(400, "username is required")
	}
	maxAge := 14 * 24 * time.Hour
	var session *models.Session
	if err := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		if existingUser, _ := transaction.GetUserByUsername(ctx, username, nil); existingUser != nil {
			return web.Error(409, "username already exists")
		}
		user := &models.User{
			Username: ptrto.Value(username),
			Password: ptrto.TrimmedString(string(hash)),
			Admin:    ptrto.Value(true),
		}
		createdUser, err := onboarding.CreateUser(ctx, transaction, user)
		if err != nil {
			return err
		}

		now := time.Now().In(time.Local)
		expiresAt := now.Add(maxAge)

		createdSession, err := transaction.CreateSession(ctx, &models.Session{
			UserID:        ptrto.Value(createdUser.ID),
			UserAgent:     ptrto.Value(request.UserAgent()),
			RemoteAddress: ptrto.Value(request.RemoteAddr),
			ExpiresAt:     ptrto.Value(expiresAt),
		}, nil)
		if err != nil {
			return err
		}
		session = createdSession
		return nil
	}); err != nil {
		if typedError, ok := err.(*web.HTTPError); ok {
			return typedError
		}
		return web.Error(500, "failed to create user")
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

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return web.Error(400, "invalid request body")
	}
	username := body.Username
	if username == "" {
		return web.Error(400, "username is required")
	}
	maxAge := 14 * 24 * time.Hour
	var session *models.Session
	if err := store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
		existingUser, err := transaction.GetUserByUsername(ctx, username, nil)
		if err != nil {
			return err
		}
		if existingUser == nil {
			return nil
		}

		if match, err := security.VerifyPassword([]byte(existingUser.GetPassword()), body.Password); err != nil || !match {
			return nil
		}

		now := time.Now().In(time.Local)
		expiresAt := now.Add(maxAge)

		createdSession, err := transaction.CreateSession(ctx, &models.Session{
			UserID:        ptrto.Value(existingUser.ID),
			UserAgent:     ptrto.Value(request.UserAgent()),
			RemoteAddress: ptrto.Value(request.RemoteAddr),
			ExpiresAt:     ptrto.Value(expiresAt),
		}, nil)
		if err != nil {
			return err
		}
		session = createdSession
		return nil
	}); err != nil {
		return web.Error(500, "failed to load users")
	}
	if session == nil {
		return web.Error(401, "invalid credentials")
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
		_ = store.StoreFromContext(request.Context()).Transaction(request.Context(), func(ctx context.Context, transaction store.Transaction) error {
			return transaction.DeleteSession(ctx, cookie.Value, nil)
		})
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

func (self *v1Api) getSessionById(ctx context.Context, sessionId string) (*models.Session, bool) {
	var session *models.Session
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		existingSession, getError := transaction.GetSession(ctx, sessionId, nil)
		if getError != nil {
			return nil
		}
		if existingSession.ExpiresAt != nil && time.Now().After(*existingSession.ExpiresAt) {
			_ = transaction.DeleteSession(ctx, sessionId, nil)
			return nil
		}
		session = existingSession
		return nil
	}); err != nil || session == nil {
		return nil, false
	}
	return session, true
}

func rateLimitBucketKeyForRemoteAddress(remoteAddress string) string {
	trimmedAddress := remoteAddress
	if trimmedAddress == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(trimmedAddress)
	if err == nil {
		trimmedHost := host
		if trimmedHost != "" {
			return trimmedHost
		}
	}
	return trimmedAddress
}

// rateLimitBucketForRemoteAddress returns the per-IP rate limit bucket for auth endpoints,
// creating one if it doesn't exist. Allows a burst of 5 requests, refilling
// at 1 request per 10 seconds.
func (self *v1Api) rateLimitBucketForRemoteAddress(remoteAddress string) *ratelimit.Bucket {
	self.rateLimitBucketsMutex.Lock()
	defer self.rateLimitBucketsMutex.Unlock()

	const maxAuthBucketEntries = 2048
	const staleEntryDuration = 24 * time.Hour

	now := time.Now()
	if len(self.rateLimitBuckets) > maxAuthBucketEntries {
		staleBefore := now.Add(-staleEntryDuration)
		for key, entry := range self.rateLimitBuckets {
			if entry.lastSeen.Before(staleBefore) {
				delete(self.rateLimitBuckets, key)
			}
		}
	}

	key := rateLimitBucketKeyForRemoteAddress(remoteAddress)
	entry, exists := self.rateLimitBuckets[key]
	if !exists {
		entry = &rateLimitBucketEntry{
			bucket: ratelimit.NewBucketWithQuantumAndInterval(1, 10*time.Second, 5),
		}
		self.rateLimitBuckets[key] = entry
	}
	entry.lastSeen = now
	return entry.bucket
}

// checkAuthRateLimit consumes one token from the per-IP auth bucket.
// Returns a 429 error if the rate limit is exceeded.
func (self *v1Api) checkAuthRateLimit(request *http.Request) error {
	bucket := self.rateLimitBucketForRemoteAddress(request.RemoteAddr)
	if bucket.TakeAvailable(1) == 0 {
		return web.Error(429, "too many requests")
	}
	return nil
}
