// Package media handles detection, storage, and retrieval of media content
// (images, videos) returned by tools such as the browser screenshot action.
package media

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
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
	case "pdf":
		return "application/pdf"
	case "mp4":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	case "svg":
		return "image/svg+xml"
	case "txt":
		return "text/plain"
	case "json":
		return "application/json"
	case "csv":
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}

// FormatFromMimeType returns a short format string from a MIME type.
func FormatFromMimeType(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	case "application/pdf":
		return "pdf"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/ogg":
		return "ogg"
	case "text/plain":
		return "txt"
	case "application/json":
		return "json"
	case "text/csv":
		return "csv"
	default:
		return ""
	}
}

// MediaMetadata holds per-file metadata persisted as a JSON sidecar.
type MediaMetadata struct {
	MediaID        string `json:"mediaId"`
	Format         string `json:"format"`
	SizeBytes      int64  `json:"sizeBytes"`
	CreatedAt      int64  `json:"createdAt"`
	SourceType     string `json:"sourceType,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	ToolName       string `json:"toolName,omitempty"`
	ToolCallID     string `json:"toolCallId,omitempty"`
	OriginalName   string `json:"originalName,omitempty"`
}

// Media bundles the saved media ID with its metadata.
type Media struct {
	MediaID  string
	Metadata MediaMetadata
}

// SaveOptions provides optional provenance fields when saving media.
type SaveOptions struct {
	SourceType     string
	AgentID        string
	ConversationID string
	ToolName       string
	ToolCallID     string
	OriginalName   string
}

// MediaFile holds an open file handle, format, and metadata.
type MediaFile struct {
	File     *os.File
	Format   string
	Metadata MediaMetadata
}

// Store manages saving and loading media files on disk.
type Store struct {
	directory string
}

// NewStore creates a new media Store backed by the given directory.
func NewStore(directory string) *Store {
	return &Store{directory: directory}
}

// metaSuffix is the extension used for metadata sidecar files.
const metaSuffix = ".meta.json"

// shardKey returns the last 2 characters of a media ID, used as the shard
// subdirectory name.
func shardKey(mediaId string) string {
	return mediaId[len(mediaId)-2:]
}

// shardedDirectory returns the shard subdirectory path for a given media ID.
func (self *Store) shardedDirectory(mediaId string) string {
	return filepath.Join(self.directory, shardKey(mediaId))
}

// Save writes data to the sharded directory {shard}/{id}.{format}, writes a
// metadata sidecar {shard}/{id}.meta.json atomically, and returns the Media.
func (self *Store) Save(data []byte, format string, options SaveOptions) (Media, error) {
	mediaId := security.NewULID()
	shardDirectory := self.shardedDirectory(mediaId)
	if err := os.MkdirAll(shardDirectory, 0755); err != nil {
		return Media{}, fmt.Errorf("creating shard directory: %w", err)
	}

	filename := fmt.Sprintf("%s.%s", mediaId, format)
	mediaPath := filepath.Join(shardDirectory, filename)
	if err := os.WriteFile(mediaPath, data, 0644); err != nil {
		return Media{}, fmt.Errorf("writing media file: %w", err)
	}

	metadata := MediaMetadata{
		MediaID:        mediaId,
		Format:         format,
		SizeBytes:      int64(len(data)),
		CreatedAt:      time.Now().UnixMilli(),
		SourceType:     options.SourceType,
		AgentID:        options.AgentID,
		ConversationID: options.ConversationID,
		ToolName:       options.ToolName,
		ToolCallID:     options.ToolCallID,
		OriginalName:   options.OriginalName,
	}

	metaPath := filepath.Join(shardDirectory, mediaId+metaSuffix)
	if err := writeMetadataToPath(metaPath, metadata); err != nil {
		os.Remove(mediaPath)
		return Media{}, fmt.Errorf("writing media metadata: %w", err)
	}

	return Media{MediaID: mediaId, Metadata: metadata}, nil
}

// Load reads a media file from disk by its ID, returning the raw bytes and
// its metadata. If the sidecar is missing, minimal metadata is lazily synthesized.
func (self *Store) Load(mediaId string) ([]byte, MediaMetadata, error) {
	mediaPath, format, err := self.findMediaFile(mediaId)
	if err != nil {
		return nil, MediaMetadata{}, err
	}
	data, err := os.ReadFile(mediaPath)
	if err != nil {
		return nil, MediaMetadata{}, fmt.Errorf("reading media file: %w", err)
	}
	metadata, err := self.loadOrSynthesizeMetadata(mediaId, format, mediaPath)
	if err != nil {
		return nil, MediaMetadata{}, err
	}
	return data, metadata, nil
}

// Open locates a media file by ID and returns an open file handle for streaming,
// along with metadata. If the sidecar is missing, minimal metadata is lazily synthesized.
func (self *Store) Open(mediaId string) (*MediaFile, error) {
	mediaPath, format, err := self.findMediaFile(mediaId)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return nil, fmt.Errorf("opening media file: %w", err)
	}
	metadata, err := self.loadOrSynthesizeMetadata(mediaId, format, mediaPath)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &MediaFile{File: file, Format: format, Metadata: metadata}, nil
}

// LoadMetadata reads the metadata sidecar for a media file. If the sidecar
// does not exist but the media file does, minimal metadata is synthesized.
func (self *Store) LoadMetadata(mediaId string) (MediaMetadata, error) {
	mediaPath, format, err := self.findMediaFile(mediaId)
	if err != nil {
		return MediaMetadata{}, err
	}
	return self.loadOrSynthesizeMetadata(mediaId, format, mediaPath)
}

// scanMediaEntry tracks a media file found during directory scanning.
type scanMediaEntry struct {
	format    string
	directory string
}

// scanMetaEntry tracks a metadata sidecar found during directory scanning.
type scanMetaEntry struct {
	mediaId   string
	directory string
}

// collectScanEntries categorizes a directory entry as either a media file or
// a metadata sidecar and adds it to the appropriate collection.
func collectScanEntries(directory string, name string, mediaFiles map[string]scanMediaEntry, metaFiles *[]scanMetaEntry) {
	if strings.HasSuffix(name, metaSuffix) {
		mediaId := strings.TrimSuffix(name, metaSuffix)
		*metaFiles = append(*metaFiles, scanMetaEntry{mediaId: mediaId, directory: directory})
		return
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	format := strings.TrimPrefix(extension, ".")
	if base != "" && format != "" {
		mediaFiles[base] = scanMediaEntry{format: format, directory: directory}
	}
}

// Scan walks the media directory (including shard subdirectories) and returns
// metadata for all files matching the filter. Files with a sidecar but no
// corresponding media file (orphan metadata) are cleaned up. Media files
// without sidecars are skipped (they will get synthesized on next Load/Open).
func (self *Store) Scan(filter func(MediaMetadata) bool) ([]MediaMetadata, error) {
	entries, err := os.ReadDir(self.directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading media directory: %w", err)
	}

	mediaFiles := make(map[string]scanMediaEntry)
	var metaFiles []scanMetaEntry

	// Collect from top-level files (legacy flat layout).
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		collectScanEntries(self.directory, entry.Name(), mediaFiles, &metaFiles)
	}

	// Collect from shard subdirectories.
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		shardDirectory := filepath.Join(self.directory, entry.Name())
		shardEntries, readError := os.ReadDir(shardDirectory)
		if readError != nil {
			continue
		}
		for _, shardEntry := range shardEntries {
			if shardEntry.IsDir() {
				continue
			}
			collectScanEntries(shardDirectory, shardEntry.Name(), mediaFiles, &metaFiles)
		}
	}

	var results []MediaMetadata

	// Process metadata files: clean up orphans, collect matches.
	for _, entry := range metaFiles {
		if _, hasMedia := mediaFiles[entry.mediaId]; !hasMedia {
			// Orphan metadata without a media file — clean up.
			metaPath := filepath.Join(entry.directory, entry.mediaId+metaSuffix)
			if err := trash.Move(metaPath, self.trashDirectory()); err != nil && !os.IsNotExist(err) {
				continue
			}
			continue
		}
		metaPath := filepath.Join(entry.directory, entry.mediaId+metaSuffix)
		metadata, readError := readMetadataFromPath(metaPath)
		if readError != nil {
			continue
		}
		if filter == nil || filter(metadata) {
			results = append(results, metadata)
		}
		// Remove from map so we know this one is accounted for.
		delete(mediaFiles, entry.mediaId)
	}

	// Remaining mediaFiles entries have no sidecar — skip them (lazy hydration on access).

	return results, nil
}

// Delete removes both the media file and its metadata sidecar from whichever
// location they reside in (sharded or flat).
func (self *Store) Delete(mediaId string) error {
	mediaPath, _, err := self.findMediaFile(mediaId)
	if err != nil {
		return err
	}
	trashDirectory := self.trashDirectory()

	if _, err := os.Stat(mediaPath); err == nil {
		if err := trash.Move(mediaPath, trashDirectory); err != nil {
			return fmt.Errorf("moving media file to trash: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stating media file: %w", err)
	}

	// Remove metadata sidecar from the same directory as the media file.
	metaPath := filepath.Join(filepath.Dir(mediaPath), mediaId+metaSuffix)
	if _, err := os.Stat(metaPath); err == nil {
		if err := trash.Move(metaPath, trashDirectory); err != nil {
			return fmt.Errorf("moving media metadata to trash: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stating media metadata: %w", err)
	}

	return nil
}

func (self *Store) trashDirectory() string {
	if filepath.Base(self.directory) == "media" {
		return filepath.Join(filepath.Dir(self.directory), ".trash")
	}
	return filepath.Join(self.directory, ".trash")
}

// findMediaFileInDirectory searches a single directory for a media file
// matching the given ID, returning the path, format, and whether it was found.
func findMediaFileInDirectory(directory string, mediaId string) (string, string, bool) {
	matches, err := filepath.Glob(filepath.Join(directory, mediaId+".*"))
	if err != nil {
		return "", "", false
	}
	for _, match := range matches {
		if strings.HasSuffix(match, metaSuffix) {
			continue
		}
		format := strings.TrimPrefix(filepath.Ext(match), ".")
		return match, format, true
	}
	return "", "", false
}

// findMediaFile locates a media file by checking the sharded path first,
// then falling back to the legacy flat layout.
func (self *Store) findMediaFile(mediaId string) (string, string, error) {
	if path, format, found := findMediaFileInDirectory(self.shardedDirectory(mediaId), mediaId); found {
		return path, format, nil
	}
	if path, format, found := findMediaFileInDirectory(self.directory, mediaId); found {
		return path, format, nil
	}
	return "", "", fmt.Errorf("media not found: %s", mediaId)
}

// readMetadataFromPath reads and parses a metadata sidecar from a specific file path.
func readMetadataFromPath(metaPath string) (MediaMetadata, error) {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return MediaMetadata{}, err
	}
	var metadata MediaMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return MediaMetadata{}, fmt.Errorf("parsing metadata: %w", err)
	}
	return metadata, nil
}

// writeMetadataToPath atomically writes a metadata sidecar to the specified path.
func writeMetadataToPath(metaPath string, metadata MediaMetadata) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	return atomicfile.WriteFile(metaPath, encoded)
}

// loadOrSynthesizeMetadata loads the sidecar from the same directory as the
// media file, or synthesizes and writes minimal metadata if the sidecar is
// missing (lazy hydration for legacy files).
func (self *Store) loadOrSynthesizeMetadata(mediaId string, format string, mediaPath string) (MediaMetadata, error) {
	metaPath := filepath.Join(filepath.Dir(mediaPath), mediaId+metaSuffix)
	metadata, err := readMetadataFromPath(metaPath)
	if err == nil {
		return metadata, nil
	}
	if !os.IsNotExist(err) {
		return MediaMetadata{}, fmt.Errorf("reading metadata for %s: %w", mediaId, err)
	}

	// Sidecar missing — synthesize from file info.
	info, statError := os.Stat(mediaPath)
	if statError != nil {
		return MediaMetadata{}, fmt.Errorf("stat media file: %w", statError)
	}

	metadata = MediaMetadata{
		MediaID:   mediaId,
		Format:    format,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime().UnixMilli(),
	}

	// Best-effort write so future loads find it.
	_ = writeMetadataToPath(metaPath, metadata)

	return metadata, nil
}
