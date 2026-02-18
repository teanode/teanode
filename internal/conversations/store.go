package conversations

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

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
)

// Store provides JSONL-based conversation persistence.
type Store struct {
	directory string
	mutex     sync.Mutex // protects file writes
}

// NewStore creates a Store that persists conversations under directory.
func NewStore(directory string) *Store {
	return &Store{directory: directory}
}

// Load reads all messages from a conversation file.
// Returns empty slice (not error) if the conversation doesn't exist.
func (self *Store) Load(conversationId string) ([]Message, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path, err := self.path(conversationId)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open conversation %s: %w", conversationId, err)
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
		if peek.Type == "conversation" || peek.Type == "session" {
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
		return nil, fmt.Errorf("reading conversation %s: %w", conversationId, err)
	}
	return messages, nil
}

// Append writes a message to the conversation file, creating it with a header if needed.
func (self *Store) Append(conversationId string, message Message) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	path, err := self.path(conversationId)
	if err != nil {
		return err
	}

	// Create file with header if it doesn't exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating conversation dir: %w", err)
		}
		header := Header{
			Type:      "conversation",
			Version:   1,
			ID:        security.NewULID(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		headerData, _ := json.Marshal(header)
		if err := atomicfile.WriteFile(path, append(headerData, '\n')); err != nil {
			return fmt.Errorf("writing conversation header: %w", err)
		}
	}

	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening conversation for append: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("appending to conversation: %w", err)
	}
	return nil
}

// Info contains a conversation id and its last activity time.
type Info struct {
	ID         string `json:"id"`
	LastActive int64  `json:"lastActive"` // ms since epoch
	Title      string `json:"title,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

// List returns all conversations, sorted by last modification time (newest first).
func (self *Store) List() ([]Info, error) {
	entries, err := os.ReadDir(self.directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing conversations: %w", err)
	}

	var conversations []Info
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		conversationInfo := Info{
			ID:         strings.TrimSuffix(entry.Name(), ".jsonl"),
			LastActive: info.ModTime().UnixMilli(),
		}
		// Read the header line to extract the title and summary.
		if header, err := self.LoadHeader(conversationInfo.ID); err == nil {
			conversationInfo.Title = header.Title
			conversationInfo.Summary = header.Summary
		}
		conversations = append(conversations, conversationInfo)
	}

	sort.Slice(conversations, func(left, right int) bool {
		return conversations[left].LastActive > conversations[right].LastActive
	})

	return conversations, nil
}

// LoadHeader reads and parses just the first line (header) of a conversation JSONL file.
func (self *Store) LoadHeader(conversationId string) (*Header, error) {
	path, err := self.path(conversationId)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open conversation %s: %w", conversationId, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return nil, fmt.Errorf("conversation %s: empty file", conversationId)
	}

	var header Header
	if err := json.Unmarshal([]byte(scanner.Text()), &header); err != nil {
		return nil, fmt.Errorf("conversation %s: parsing header: %w", conversationId, err)
	}
	return &header, nil
}

// SetTitleAndSummary updates both the title and summary in a conversation's header
// line in a single write, preserving the file's original modification time.
func (self *Store) SetTitleAndSummary(conversationId, title, summary string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return self.updateHeader(conversationId, func(header *Header) {
		header.Title = title
		header.Summary = summary
		header.SummarizedAt = time.Now().UnixMilli()
	})
}

// updateHeader reads the conversation file, applies mutate to the header, rewrites
// the file, and restores the original modification time.
func (self *Store) updateHeader(conversationId string, mutate func(*Header)) error {
	path, err := self.path(conversationId)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat conversation %s: %w", conversationId, err)
	}
	originalModTime := info.ModTime()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading conversation %s: %w", conversationId, err)
	}

	// Split into first line (header) and the rest.
	index := bytes.IndexByte(data, '\n')
	if index < 0 {
		return fmt.Errorf("conversation %s: no newline in file", conversationId)
	}

	var header Header
	if err := json.Unmarshal(data[:index], &header); err != nil {
		return fmt.Errorf("conversation %s: parsing header: %w", conversationId, err)
	}
	mutate(&header)

	newHeader, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("conversation %s: marshaling header: %w", conversationId, err)
	}

	var buffer bytes.Buffer
	buffer.Write(newHeader)
	buffer.Write(data[index:]) // includes the leading '\n' and all remaining lines
	if err := atomicfile.WriteFile(path, buffer.Bytes()); err != nil {
		return err
	}

	// Restore original modification time so LastActive isn't affected.
	return os.Chtimes(path, originalModTime, originalModTime)
}

// Delete removes a conversation file.
func (self *Store) Delete(conversationId string) error {
	path, err := self.path(conversationId)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// PageResult holds a page of messages plus pagination metadata.
type PageResult struct {
	Messages          []Message `json:"messages"`
	TotalCount        int       `json:"totalCount"`
	OldestLoadedIndex int       `json:"oldestLoadedIndex"`
	HasMore           bool      `json:"hasMore"`
}

// LoadPage returns a page of messages from a conversation.
// It loads the full conversation and slices in memory — the savings come from
// sending fewer messages over the wire to the frontend.
//
// If beforeIndex <= 0, the last `limit` messages are returned.
// Otherwise, `limit` messages ending just before `beforeIndex` are returned.
func (self *Store) LoadPage(conversationId string, limit, beforeIndex int) (*PageResult, error) {
	messages, err := self.Load(conversationId)
	if err != nil {
		return nil, err
	}
	if messages == nil {
		return &PageResult{Messages: []Message{}}, nil
	}

	totalCount := len(messages)

	// Determine the slice end (exclusive upper bound).
	end := totalCount
	if beforeIndex > 0 && beforeIndex < totalCount {
		end = beforeIndex
	}

	// Determine the slice start.
	start := end - limit
	if start < 0 {
		start = 0
	}

	return &PageResult{
		Messages:          messages[start:end],
		TotalCount:        totalCount,
		OldestLoadedIndex: start,
		HasMore:           start > 0,
	}, nil
}

var errEmptyConversationId = fmt.Errorf("conversation id must not be empty")

func (self *Store) path(conversationId string) (string, error) {
	if conversationId == "" {
		return "", errEmptyConversationId
	}
	return filepath.Join(self.directory, conversationId+".jsonl"), nil
}
