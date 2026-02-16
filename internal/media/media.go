// Package media handles detection, storage, and retrieval of media content
// (images, videos) returned by tools such as browser_screenshot.
package media

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/internal/util/ulid"
)

// MediaContent represents detected media in a tool result.
type MediaContent struct {
	Base64  string `json:"base64,omitempty"`
	Format  string `json:"format,omitempty"`
	MediaID string `json:"mediaId,omitempty"`
}

// DetectMedia parses a JSON tool result and returns non-nil if the shape
// matches {"base64": ..., "format": ...} or {"mediaId": ..., "format": ...}.
func DetectMedia(toolResult string) *MediaContent {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(toolResult), &raw); err != nil {
		return nil
	}

	format, _ := raw["format"].(string)
	if format == "" {
		return nil
	}

	if base64Data, ok := raw["base64"].(string); ok && base64Data != "" {
		return &MediaContent{Base64: base64Data, Format: format}
	}
	if mediaId, ok := raw["mediaId"].(string); ok && mediaId != "" {
		return &MediaContent{MediaID: mediaId, Format: format}
	}
	return nil
}

// IsImageFormat returns true if the format string represents an image type.
func IsImageFormat(format string) bool {
	switch strings.ToLower(format) {
	case "png", "jpeg", "jpg", "gif", "webp":
		return true
	}
	return false
}

// MimeType returns the MIME type string for a given format.
func MimeType(format string) string {
	switch strings.ToLower(format) {
	case "png":
		return "image/png"
	case "jpeg", "jpg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// Store manages saving and loading media files on disk.
type Store struct {
	directory string
}

// NewStore creates a new media Store backed by the given directory.
func NewStore(directory string) *Store {
	return &Store{directory: directory}
}

// Save writes data to disk as {id}.{format} and returns the media ID.
func (self *Store) Save(data []byte, format string) (string, error) {
	mediaId := ulid.GenerateString()
	filename := fmt.Sprintf("%s.%s", mediaId, format)
	path := filepath.Join(self.directory, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("writing media file: %w", err)
	}
	return mediaId, nil
}

// Load reads a media file from disk by its ID, returning the raw bytes and
// the format inferred from the file extension.
func (self *Store) Load(mediaId string) ([]byte, string, error) {
	// Find the file by globbing for the mediaId prefix.
	matches, err := filepath.Glob(filepath.Join(self.directory, mediaId+".*"))
	if err != nil {
		return nil, "", fmt.Errorf("searching for media file: %w", err)
	}
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("media not found: %s", mediaId)
	}
	path := matches[0]
	extension := strings.TrimPrefix(filepath.Ext(path), ".")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("reading media file: %w", err)
	}
	return data, extension, nil
}

// MediaFile holds an open file handle and the format inferred from the extension.
type MediaFile struct {
	File   *os.File
	Format string
}

// Open locates a media file by ID and returns an open file handle for streaming.
func (self *Store) Open(mediaId string) (*MediaFile, error) {
	matches, err := filepath.Glob(filepath.Join(self.directory, mediaId+".*"))
	if err != nil {
		return nil, fmt.Errorf("searching for media file: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("media not found: %s", mediaId)
	}
	path := matches[0]
	format := strings.TrimPrefix(filepath.Ext(path), ".")
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening media file: %w", err)
	}
	return &MediaFile{File: file, Format: format}, nil
}
