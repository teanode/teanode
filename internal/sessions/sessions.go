// Package sessions provides file-based session storage for the web login system.
// Each session is stored as a YAML file at ~/.teanode/sessions/<ulid>.yaml.
package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"gopkg.in/yaml.v3"
)

// Session represents a single authenticated browser session.
type Session struct {
	ID         string    `json:"id" yaml:"id"`
	CreatedAt  time.Time `json:"createdAt" yaml:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt" yaml:"expiresAt"`
	UserAgent  string    `json:"userAgent" yaml:"userAgent"`
	RemoteAddr string    `json:"remoteAddr" yaml:"remoteAddr"`
	LastSeenAt time.Time `json:"lastSeenAt" yaml:"lastSeenAt"`
}

// Store provides thread-safe persistence for sessions.
type Store struct {
	directory string
	mutex     sync.Mutex
}

// NewStore creates a Store that persists to the given directory.
func NewStore(directory string) *Store {
	return &Store{directory: directory}
}

// Create generates a new session and writes it to disk.
func (self *Store) Create(userAgent, remoteAddr string, maxAge time.Duration) (*Session, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	now := time.Now()
	session := &Session{
		ID:         security.NewULID(),
		CreatedAt:  now,
		ExpiresAt:  now.Add(maxAge),
		UserAgent:  userAgent,
		RemoteAddr: remoteAddr,
		LastSeenAt: now,
	}

	if err := self.writeSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// Get retrieves a session by ID. Returns nil if not found or expired.
func (self *Store) Get(id string) *Session {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	session, err := self.readSession(id)
	if err != nil {
		return nil
	}
	if time.Now().After(session.ExpiresAt) {
		// Expired — clean up.
		os.Remove(self.sessionPath(id))
		return nil
	}
	return session
}

// Touch renews the session's ExpiresAt and LastSeenAt. Throttled to once per
// hour to avoid excessive disk writes.
func (self *Store) Touch(id string, maxAge time.Duration) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	session, err := self.readSession(id)
	if err != nil {
		return
	}

	// Throttle: only update if last seen was more than an hour ago.
	if time.Since(session.LastSeenAt) < time.Hour {
		return
	}

	now := time.Now()
	session.ExpiresAt = now.Add(maxAge)
	session.LastSeenAt = now
	self.writeSession(session)
}

// Delete removes a session file.
func (self *Store) Delete(id string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path := self.sessionPath(id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("session not found: %s", id)
	}
	return os.Remove(path)
}

// List returns all non-expired sessions.
func (self *Store) List() ([]*Session, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	entries, err := os.ReadDir(self.directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading sessions directory: %w", err)
	}

	now := time.Now()
	var sessions []*Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		session, err := self.readSession(id)
		if err != nil {
			continue
		}
		if now.After(session.ExpiresAt) {
			os.Remove(self.sessionPath(id))
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (self *Store) sessionPath(id string) string {
	return filepath.Join(self.directory, id+".yaml")
}

func (self *Store) readSession(id string) (*Session, error) {
	data, err := os.ReadFile(self.sessionPath(id))
	if err != nil {
		return nil, err
	}
	var session Session
	if err := yaml.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (self *Store) writeSession(session *Session) error {
	data, err := yaml.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshalling session: %w", err)
	}
	return atomicfile.WriteFile(self.sessionPath(session.ID), data)
}
