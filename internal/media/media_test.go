package media

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveWritesToShardDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	saved, err := store.Save([]byte("shard test data"), "png", SaveOptions{SourceType: "tool"})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if saved.MediaID == "" {
		t.Fatal("Save returned empty MediaID")
	}

	// Verify the file lands in a shard subdirectory named by the last 2 chars.
	shard := shardKey(saved.MediaID)
	shardDirectory := filepath.Join(directory, shard)

	mediaPath := filepath.Join(shardDirectory, saved.MediaID+".png")
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("media file not found in shard directory %q: %v", shard, err)
	}

	metaPath := filepath.Join(shardDirectory, saved.MediaID+metaSuffix)
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("meta file not found in shard directory %q: %v", shard, err)
	}

	// Verify no files at the top level.
	flatMedia := filepath.Join(directory, saved.MediaID+".png")
	if _, err := os.Stat(flatMedia); !os.IsNotExist(err) {
		t.Error("media file should NOT exist at the flat top level")
	}
}

func TestSaveAndLoadMetadata(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	data := []byte("fake png data")
	options := SaveOptions{
		SourceType:     "tool",
		AgentID:        "agent-1",
		ConversationID: "conv-1",
		ToolName:       "screenshot",
		ToolCallID:     "call-1",
	}

	saved, err := store.Save(data, "png", options)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if saved.MediaID == "" {
		t.Fatal("Save returned empty MediaID")
	}
	if saved.Metadata.Format != "png" {
		t.Errorf("metadata format = %q, want %q", saved.Metadata.Format, "png")
	}
	if saved.Metadata.SizeBytes != int64(len(data)) {
		t.Errorf("metadata sizeBytes = %d, want %d", saved.Metadata.SizeBytes, len(data))
	}
	if saved.Metadata.SourceType != "tool" {
		t.Errorf("metadata sourceType = %q, want %q", saved.Metadata.SourceType, "tool")
	}
	if saved.Metadata.AgentID != "agent-1" {
		t.Errorf("metadata agentId = %q, want %q", saved.Metadata.AgentID, "agent-1")
	}
	if saved.Metadata.ConversationID != "conv-1" {
		t.Errorf("metadata conversationId = %q, want %q", saved.Metadata.ConversationID, "conv-1")
	}
	if saved.Metadata.ToolName != "screenshot" {
		t.Errorf("metadata toolName = %q, want %q", saved.Metadata.ToolName, "screenshot")
	}
	if saved.Metadata.ToolCallID != "call-1" {
		t.Errorf("metadata toolCallId = %q, want %q", saved.Metadata.ToolCallID, "call-1")
	}
	if saved.Metadata.CreatedAt == 0 {
		t.Error("metadata createdAt should be non-zero")
	}

	// Load the data back.
	loadedData, loadedMetadata, err := store.Load(saved.MediaID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(loadedData) != string(data) {
		t.Errorf("loaded data = %q, want %q", loadedData, data)
	}
	if loadedMetadata.MediaID != saved.MediaID {
		t.Errorf("loaded metadata mediaId = %q, want %q", loadedMetadata.MediaID, saved.MediaID)
	}
	if loadedMetadata.Format != "png" {
		t.Errorf("loaded metadata format = %q, want %q", loadedMetadata.Format, "png")
	}
	if loadedMetadata.AgentID != "agent-1" {
		t.Errorf("loaded metadata agentId = %q, want %q", loadedMetadata.AgentID, "agent-1")
	}

	// LoadMetadata independently.
	metadata, err := store.LoadMetadata(saved.MediaID)
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}
	if metadata.ToolName != "screenshot" {
		t.Errorf("LoadMetadata toolName = %q, want %q", metadata.ToolName, "screenshot")
	}
}

func TestLoadLegacyFlatLayout(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Place a media file + sidecar at the top level (legacy flat layout).
	mediaId := "01aryz6dfw3jqftg2s41cyb8a3"
	mediaContent := []byte("legacy flat data")
	if err := os.WriteFile(filepath.Join(directory, mediaId+".png"), mediaContent, 0644); err != nil {
		t.Fatalf("creating legacy media file: %v", err)
	}
	legacyMeta := MediaMetadata{MediaID: mediaId, Format: "png", SizeBytes: int64(len(mediaContent))}
	encoded, _ := json.Marshal(legacyMeta)
	if err := os.WriteFile(filepath.Join(directory, mediaId+metaSuffix), encoded, 0644); err != nil {
		t.Fatalf("creating legacy meta file: %v", err)
	}

	// Load should find the flat-layout file.
	data, metadata, err := store.Load(mediaId)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(data) != string(mediaContent) {
		t.Errorf("data mismatch: got %q, want %q", data, mediaContent)
	}
	if metadata.MediaID != mediaId {
		t.Errorf("metadata mediaId = %q, want %q", metadata.MediaID, mediaId)
	}
	if metadata.Format != "png" {
		t.Errorf("metadata format = %q, want %q", metadata.Format, "png")
	}
}

func TestOpenLegacyFlatLayout(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	mediaId := "01aryz6dfw3jqftg2s41cyb8a3"
	mediaContent := []byte("legacy open data")
	if err := os.WriteFile(filepath.Join(directory, mediaId+".jpeg"), mediaContent, 0644); err != nil {
		t.Fatalf("creating legacy file: %v", err)
	}

	mediaFile, err := store.Open(mediaId)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer mediaFile.File.Close()

	if mediaFile.Format != "jpeg" {
		t.Errorf("format = %q, want %q", mediaFile.Format, "jpeg")
	}
	if mediaFile.Metadata.MediaID != mediaId {
		t.Errorf("metadata mediaId = %q, want %q", mediaFile.Metadata.MediaID, mediaId)
	}
}

func TestLazyMetadataHydration(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a legacy media file without a sidecar (flat layout).
	mediaId := "01aryz6dfw3jqftg2s41cyb8a3"
	mediaContent := []byte("legacy image data")
	mediaPath := filepath.Join(directory, mediaId+".png")
	if err := os.WriteFile(mediaPath, mediaContent, 0644); err != nil {
		t.Fatalf("creating legacy file: %v", err)
	}

	// Load should succeed and synthesize metadata.
	data, metadata, err := store.Load(mediaId)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(data) != string(mediaContent) {
		t.Errorf("data mismatch")
	}
	if metadata.MediaID != mediaId {
		t.Errorf("metadata mediaId = %q, want %q", metadata.MediaID, mediaId)
	}
	if metadata.Format != "png" {
		t.Errorf("metadata format = %q, want %q", metadata.Format, "png")
	}
	if metadata.SizeBytes != int64(len(mediaContent)) {
		t.Errorf("metadata sizeBytes = %d, want %d", metadata.SizeBytes, len(mediaContent))
	}
	if metadata.SourceType != "" {
		t.Errorf("metadata sourceType = %q, want empty", metadata.SourceType)
	}

	// The sidecar should now exist in the same directory as the media file.
	sidecarPath := filepath.Join(directory, mediaId+metaSuffix)
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Errorf("sidecar not written after lazy hydration: %v", err)
	}

	// Open should also work with lazy hydration.
	os.Remove(sidecarPath)
	mediaFile, err := store.Open(mediaId)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer mediaFile.File.Close()
	if mediaFile.Format != "png" {
		t.Errorf("Open format = %q, want %q", mediaFile.Format, "png")
	}
	if mediaFile.Metadata.MediaID != mediaId {
		t.Errorf("Open metadata mediaId = %q, want %q", mediaFile.Metadata.MediaID, mediaId)
	}

	// LoadMetadata should also synthesize.
	os.Remove(sidecarPath)
	loadedMetadata, err := store.LoadMetadata(mediaId)
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}
	if loadedMetadata.Format != "png" {
		t.Errorf("LoadMetadata format = %q, want %q", loadedMetadata.Format, "png")
	}
}

func TestLazyMetadataHydrationInShardDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a media file in the shard directory WITHOUT a sidecar.
	mediaId := "01aryz6dfw3jqftg2s41cyb8a3"
	shard := shardKey(mediaId)
	shardDirectory := filepath.Join(directory, shard)
	if err := os.MkdirAll(shardDirectory, 0755); err != nil {
		t.Fatalf("creating shard dir: %v", err)
	}
	mediaContent := []byte("sharded no-meta data")
	if err := os.WriteFile(filepath.Join(shardDirectory, mediaId+".png"), mediaContent, 0644); err != nil {
		t.Fatalf("creating sharded file: %v", err)
	}

	// Load should find the file in the shard dir and synthesize metadata there.
	data, metadata, err := store.Load(mediaId)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(data) != string(mediaContent) {
		t.Error("data mismatch")
	}
	if metadata.Format != "png" {
		t.Errorf("format = %q, want %q", metadata.Format, "png")
	}

	// Sidecar should be written into the shard directory, not the top level.
	if _, err := os.Stat(filepath.Join(shardDirectory, mediaId+metaSuffix)); err != nil {
		t.Error("sidecar should exist in shard directory after lazy hydration")
	}
	if _, err := os.Stat(filepath.Join(directory, mediaId+metaSuffix)); !os.IsNotExist(err) {
		t.Error("sidecar should NOT exist at the top level")
	}
}

func TestScanBothFlatAndShardedFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Create a legacy flat file with sidecar.
	flatId := "01flat0000000000000000flat"
	flatContent := []byte("flat data")
	if err := os.WriteFile(filepath.Join(directory, flatId+".png"), flatContent, 0644); err != nil {
		t.Fatalf("creating flat media: %v", err)
	}
	flatMeta := MediaMetadata{MediaID: flatId, Format: "png", SizeBytes: int64(len(flatContent)), SourceType: "legacy"}
	encoded, _ := json.Marshal(flatMeta)
	if err := os.WriteFile(filepath.Join(directory, flatId+metaSuffix), encoded, 0644); err != nil {
		t.Fatalf("creating flat meta: %v", err)
	}

	// Save a new file (goes to shard dir).
	saved, err := store.Save([]byte("sharded data"), "jpeg", SaveOptions{SourceType: "tool"})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Scan should see both.
	all, err := store.Scan(nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("Scan returned %d items, want 2", len(all))
	}

	// Verify both IDs are present.
	found := map[string]bool{}
	for _, metadata := range all {
		found[metadata.MediaID] = true
	}
	if !found[flatId] {
		t.Error("Scan did not find flat-layout file")
	}
	if !found[saved.MediaID] {
		t.Error("Scan did not find sharded file")
	}
}

func TestScanFiltering(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	// Save several files (all go to shard directories).
	_, err := store.Save([]byte("png data"), "png", SaveOptions{SourceType: "tool", ToolName: "screenshot"})
	if err != nil {
		t.Fatalf("Save 1 failed: %v", err)
	}
	_, err = store.Save([]byte("jpeg data"), "jpeg", SaveOptions{SourceType: "tool", ToolName: "camera"})
	if err != nil {
		t.Fatalf("Save 2 failed: %v", err)
	}
	_, err = store.Save([]byte("gif data"), "gif", SaveOptions{SourceType: "upload"})
	if err != nil {
		t.Fatalf("Save 3 failed: %v", err)
	}

	// Scan all.
	all, err := store.Scan(nil)
	if err != nil {
		t.Fatalf("Scan all failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("Scan all returned %d items, want 3", len(all))
	}

	// Scan with filter.
	tools, err := store.Scan(func(metadata MediaMetadata) bool {
		return metadata.SourceType == "tool"
	})
	if err != nil {
		t.Fatalf("Scan filtered failed: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("Scan filtered returned %d items, want 2", len(tools))
	}

	// Scan with format filter.
	pngs, err := store.Scan(func(metadata MediaMetadata) bool {
		return metadata.Format == "png"
	})
	if err != nil {
		t.Fatalf("Scan png failed: %v", err)
	}
	if len(pngs) != 1 {
		t.Errorf("Scan png returned %d items, want 1", len(pngs))
	}
}

func TestScanOrphanCleanup(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Create an orphan metadata sidecar at the top level with no corresponding media file.
	orphanId := "01orphan000000000000000000"
	orphanMeta := MediaMetadata{MediaID: orphanId, Format: "png"}
	encoded, _ := json.Marshal(orphanMeta)
	orphanPath := filepath.Join(directory, orphanId+metaSuffix)
	if err := os.WriteFile(orphanPath, encoded, 0644); err != nil {
		t.Fatalf("writing orphan: %v", err)
	}

	// Also create a valid pair via Save (goes to shard dir).
	_, err := store.Save([]byte("valid data"), "png", SaveOptions{})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	results, err := store.Scan(nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Scan returned %d items, want 1 (orphan should be excluded)", len(results))
	}

	// Orphan sidecar should have been cleaned up.
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan metadata sidecar should have been removed")
	}
}

func TestScanOrphanCleanupInShardDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Create an orphan metadata sidecar inside a shard directory.
	orphanId := "01orphan000000000000000xyz"
	shard := shardKey(orphanId)
	shardDirectory := filepath.Join(directory, shard)
	if err := os.MkdirAll(shardDirectory, 0755); err != nil {
		t.Fatalf("creating shard dir: %v", err)
	}
	orphanMeta := MediaMetadata{MediaID: orphanId, Format: "png"}
	encoded, _ := json.Marshal(orphanMeta)
	orphanPath := filepath.Join(shardDirectory, orphanId+metaSuffix)
	if err := os.WriteFile(orphanPath, encoded, 0644); err != nil {
		t.Fatalf("writing orphan in shard: %v", err)
	}

	results, err := store.Scan(nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Scan returned %d items, want 0", len(results))
	}

	// Orphan should be cleaned up from the shard directory.
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("orphan metadata in shard directory should have been removed")
	}
}

func TestDeleteShardedFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	saved, err := store.Save([]byte("to delete"), "png", SaveOptions{SourceType: "test"})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	shard := shardKey(saved.MediaID)
	shardDirectory := filepath.Join(directory, shard)
	mediaPath := filepath.Join(shardDirectory, saved.MediaID+".png")
	metaPath := filepath.Join(shardDirectory, saved.MediaID+metaSuffix)

	// Verify both files exist in shard.
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("media file missing before delete: %v", err)
	}
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("meta file missing before delete: %v", err)
	}

	// Delete.
	if err := store.Delete(saved.MediaID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify both removed.
	if _, err := os.Stat(mediaPath); !os.IsNotExist(err) {
		t.Error("media file should not exist after delete")
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("meta file should not exist after delete")
	}
}

func TestDeleteLegacyFlatFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	// Place a media file + sidecar at the top level (legacy flat).
	mediaId := "01legacy00000000000000flat"
	mediaContent := []byte("legacy delete data")
	mediaPath := filepath.Join(directory, mediaId+".png")
	metaPath := filepath.Join(directory, mediaId+metaSuffix)
	if err := os.WriteFile(mediaPath, mediaContent, 0644); err != nil {
		t.Fatalf("creating flat media: %v", err)
	}
	flatMeta := MediaMetadata{MediaID: mediaId, Format: "png"}
	encoded, _ := json.Marshal(flatMeta)
	if err := os.WriteFile(metaPath, encoded, 0644); err != nil {
		t.Fatalf("creating flat meta: %v", err)
	}

	// Delete should find and remove the flat-layout files.
	if err := store.Delete(mediaId); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(mediaPath); !os.IsNotExist(err) {
		t.Error("flat media file should not exist after delete")
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Error("flat meta file should not exist after delete")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	if err := store.Delete("nonexistent"); err == nil {
		t.Error("Delete of non-existent media should error")
	}
}

func TestSaveCreatesMetaSidecar(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store := NewStore(directory)

	saved, err := store.Save([]byte("data"), "webp", SaveOptions{})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the sidecar exists in the shard directory and is valid JSON.
	shard := shardKey(saved.MediaID)
	sidecarPath := filepath.Join(directory, shard, saved.MediaID+metaSuffix)
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("reading sidecar: %v", err)
	}
	var metadata MediaMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("parsing sidecar JSON: %v", err)
	}
	if metadata.MediaID != saved.MediaID {
		t.Errorf("sidecar mediaId = %q, want %q", metadata.MediaID, saved.MediaID)
	}
	if metadata.Format != "webp" {
		t.Errorf("sidecar format = %q, want %q", metadata.Format, "webp")
	}
}

func TestOpenReturnsMetadata(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	saved, err := store.Save([]byte("open test"), "jpeg", SaveOptions{
		ToolName: "browser",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	mediaFile, err := store.Open(saved.MediaID)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer mediaFile.File.Close()

	if mediaFile.Format != "jpeg" {
		t.Errorf("Open format = %q, want %q", mediaFile.Format, "jpeg")
	}
	if mediaFile.Metadata.ToolName != "browser" {
		t.Errorf("Open metadata toolName = %q, want %q", mediaFile.Metadata.ToolName, "browser")
	}
}

func TestLoadNonExistent(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	_, _, err := store.Load("nonexistent")
	if err == nil {
		t.Error("Load of non-existent media should error")
	}
}

func TestScanEmptyDirectory(t *testing.T) {
	t.Parallel()
	store := NewStore(t.TempDir())

	results, err := store.Scan(nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Scan returned %d items, want 0", len(results))
	}
}

func TestScanNonExistentDirectory(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "nonexistent"))

	results, err := store.Scan(nil)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if results != nil {
		t.Errorf("Scan returned non-nil for nonexistent directory")
	}
}

func TestShardKeyUsesLastTwoCharacters(t *testing.T) {
	t.Parallel()
	if got := shardKey("01aryz6dfw3jqftg2s41cyb8a3"); got != "a3" {
		t.Errorf("shardKey = %q, want %q", got, "a3")
	}
	if got := shardKey("01abc"); got != "bc" {
		t.Errorf("shardKey = %q, want %q", got, "bc")
	}
}

func TestDetectMedia(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect *MediaContent
	}{
		{
			name:   "base64 media",
			input:  `{"base64":"aGVsbG8=","format":"png"}`,
			expect: &MediaContent{Base64: "aGVsbG8=", Format: "png"},
		},
		{
			name:   "mediaId reference",
			input:  `{"mediaId":"abc123","format":"jpeg"}`,
			expect: &MediaContent{MediaID: "abc123", Format: "jpeg"},
		},
		{
			name:   "no format",
			input:  `{"base64":"aGVsbG8="}`,
			expect: nil,
		},
		{
			name:   "invalid json",
			input:  `not json`,
			expect: nil,
		},
		{
			name:   "empty base64",
			input:  `{"base64":"","format":"png"}`,
			expect: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := DetectMedia(testCase.input)
			if testCase.expect == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if result.Base64 != testCase.expect.Base64 {
				t.Errorf("base64 = %q, want %q", result.Base64, testCase.expect.Base64)
			}
			if result.Format != testCase.expect.Format {
				t.Errorf("format = %q, want %q", result.Format, testCase.expect.Format)
			}
			if result.MediaID != testCase.expect.MediaID {
				t.Errorf("mediaId = %q, want %q", result.MediaID, testCase.expect.MediaID)
			}
		})
	}
}

func TestIsImageFormat(t *testing.T) {
	t.Parallel()

	positives := []string{"png", "jpeg", "jpg", "gif", "webp", "PNG", "JPEG"}
	for _, format := range positives {
		if !IsImageFormat(format) {
			t.Errorf("IsImageFormat(%q) = false, want true", format)
		}
	}

	negatives := []string{"pdf", "mp4", "txt", ""}
	for _, format := range negatives {
		if IsImageFormat(format) {
			t.Errorf("IsImageFormat(%q) = true, want false", format)
		}
	}
}

func TestMimeType(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"png":     "image/png",
		"jpeg":    "image/jpeg",
		"jpg":     "image/jpeg",
		"gif":     "image/gif",
		"webp":    "image/webp",
		"unknown": "application/octet-stream",
	}
	for format, expected := range tests {
		result := MimeType(format)
		if result != expected {
			t.Errorf("MimeType(%q) = %q, want %q", format, result, expected)
		}
	}
}
