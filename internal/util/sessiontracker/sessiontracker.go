// Package sessiontracker tracks active session connections using reference counting.
package sessiontracker

import "sync"

// SessionTracker tracks active session connections using reference counting.
// Multiple connections for the same session ID are supported.
type SessionTracker struct {
	mutex    sync.RWMutex
	sessions map[string]int // sessionId -> active connection count
}

// New creates a new SessionTracker.
func New() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]int),
	}
}

// MarkConnected increments the connection count for a session.
func (self *SessionTracker) MarkConnected(sessionId string) {
	if sessionId == "" {
		return
	}
	self.mutex.Lock()
	self.sessions[sessionId]++
	self.mutex.Unlock()
}

// MarkDisconnected decrements the connection count for a session.
func (self *SessionTracker) MarkDisconnected(sessionId string) {
	if sessionId == "" {
		return
	}
	self.mutex.Lock()
	if count, ok := self.sessions[sessionId]; ok {
		if count <= 1 {
			delete(self.sessions, sessionId)
		} else {
			self.sessions[sessionId] = count - 1
		}
	}
	self.mutex.Unlock()
}

// IsConnected returns true if the session has at least one active connection.
func (self *SessionTracker) IsConnected(sessionId string) bool {
	if sessionId == "" {
		return false
	}
	self.mutex.RLock()
	defer self.mutex.RUnlock()
	return self.sessions[sessionId] > 0
}
