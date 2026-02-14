package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/teanode/teanode/internal/config"
	"github.com/teanode/teanode/internal/util/atomicfile"
)

// Store provides JSONL-based session persistence.
type Store struct {
	directory string
	mutex     sync.Mutex // protects file writes
}

// NewStore creates a Store that persists sessions under directory.
func NewStore(directory string) *Store {
	return &Store{directory: directory}
}

// NewStoreDefault creates a Store using the default sessions directory.
func NewStoreDefault() (*Store, error) {
	directory, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	return NewStore(directory), nil
}

// Load reads all messages from a session file.
// Returns empty slice (not error) if the session doesn't exist.
func (self *Store) Load(sessionKey string) ([]Message, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path := self.path(sessionKey)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open session %s: %w", sessionKey, err)
	}
	defer file.Close()

	var messages []Message
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // up to 1MB lines
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Peek at the type field to decide how to parse.
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &peek); err != nil {
			continue // skip malformed lines
		}
		if peek.Type == "session" {
			// Header line — skip for message loading
			continue
		}

		var message Message
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			continue
		}
		if message.Role != "" {
			messages = append(messages, message)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading session %s: %w", sessionKey, err)
	}
	return messages, nil
}

// Append writes a message to the session file, creating it with a header if needed.
func (self *Store) Append(sessionKey string, message Message) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path := self.path(sessionKey)

	// Create file with header if it doesn't exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating session dir: %w", err)
		}
		header := SessionHeader{
			Type:      "session",
			Version:   1,
			ID:        uuid.New().String(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		headerData, _ := json.Marshal(header)
		if err := atomicfile.WriteFile(path, append(headerData, '\n')); err != nil {
			return fmt.Errorf("writing session header: %w", err)
		}
	}

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening session for append: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("appending to session: %w", err)
	}
	return nil
}

// SessionInfo contains a session key and its last activity time.
type SessionInfo struct {
	Key        string `json:"key"`
	LastActive int64  `json:"lastActive"` // ms since epoch
	Title      string `json:"title,omitempty"`
}

// List returns all session keys, sorted by last modification time (newest first).
func (self *Store) List() ([]SessionInfo, error) {
	entries, err := os.ReadDir(self.directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	var sessions []SessionInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sessionInfo := SessionInfo{
			Key:        strings.TrimSuffix(entry.Name(), ".jsonl"),
			LastActive: info.ModTime().UnixMilli(),
		}
		// Read the header line to extract the title.
		if header, err := self.LoadHeader(sessionInfo.Key); err == nil {
			sessionInfo.Title = header.Title
		}
		sessions = append(sessions, sessionInfo)
	}

	sort.Slice(sessions, func(left, right int) bool {
		return sessions[left].LastActive > sessions[right].LastActive
	})

	return sessions, nil
}

// LoadHeader reads and parses just the first line (header) of a session JSONL file.
func (self *Store) LoadHeader(sessionKey string) (*SessionHeader, error) {
	path := self.path(sessionKey)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session %s: %w", sessionKey, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return nil, fmt.Errorf("session %s: empty file", sessionKey)
	}

	var header SessionHeader
	if err := json.Unmarshal([]byte(scanner.Text()), &header); err != nil {
		return nil, fmt.Errorf("session %s: parsing header: %w", sessionKey, err)
	}
	return &header, nil
}

// SetTitle updates the title in a session's header line.
func (self *Store) SetTitle(sessionKey, title string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path := self.path(sessionKey)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading session %s: %w", sessionKey, err)
	}

	// Split into first line (header) and the rest.
	index := bytes.IndexByte(data, '\n')
	if index < 0 {
		return fmt.Errorf("session %s: no newline in file", sessionKey)
	}

	var header SessionHeader
	if err := json.Unmarshal(data[:index], &header); err != nil {
		return fmt.Errorf("session %s: parsing header: %w", sessionKey, err)
	}
	header.Title = title

	newHeader, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("session %s: marshaling header: %w", sessionKey, err)
	}

	var buffer bytes.Buffer
	buffer.Write(newHeader)
	buffer.Write(data[index:]) // includes the leading '\n' and all remaining lines
	return atomicfile.WriteFile(path, buffer.Bytes())
}

// Delete removes a session file.
func (self *Store) Delete(sessionKey string) error {
	path := self.path(sessionKey)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (self *Store) path(sessionKey string) string {
	return filepath.Join(self.directory, sessionKey+".jsonl")
}
