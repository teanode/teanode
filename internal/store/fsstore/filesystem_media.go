package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/mimetypes"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
)

const mediaMetadataSuffix = ".meta.json"

type storeMediaMetadata struct {
	MediaID        string `json:"mediaId"`
	UserID         string `json:"userId,omitempty"`
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

func (self *fileSystemTransaction) ListMedia(ctx context.Context, listOptions store.MediaListOptions, options *store.Option) ([]*models.Media, error) {
	return self.listMedia(listOptions, options)
}

func (self *fileSystemTransaction) CreateMedia(ctx context.Context, content io.Reader, metadata *models.Media, options *store.Option) (*models.Media, error) {
	return self.createMedia(content, metadata, options)
}

func (self *fileSystemTransaction) GetMedia(ctx context.Context, mediaId string, options *store.Option) ([]byte, *models.Media, error) {
	return self.getMedia(mediaId, options)
}

func (self *fileSystemTransaction) OpenMedia(ctx context.Context, mediaId string, options *store.Option) (io.ReadCloser, *models.Media, error) {
	return self.openMedia(mediaId, options)
}

func (self *fileSystemTransaction) ModifyMedia(ctx context.Context, mediaId string, modifier func(*models.Media) error, options *store.Option) (*models.Media, error) {
	return self.modifyMedia(ctx, mediaId, modifier, options)
}

func (self *fileSystemTransaction) DeleteMedia(ctx context.Context, mediaId string, options *store.Option) error {
	return self.deleteMedia(mediaId, options)
}

func (self *fileSystemTransaction) listMedia(listOptions store.MediaListOptions, options *store.Option) ([]*models.Media, error) {
	metadataList, err := self.scanMediaMetadata(func(metadata storeMediaMetadata) bool {
		if listOptions.UserID != nil && metadata.UserID != *listOptions.UserID {
			return false
		}
		if listOptions.ConversationID != nil && metadata.ConversationID != *listOptions.ConversationID {
			return false
		}
		if listOptions.Source != nil && metadata.SourceType != *listOptions.Source {
			return false
		}
		if listOptions.ToolName != nil && metadata.ToolName != *listOptions.ToolName {
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	results := make([]*models.Media, 0, len(metadataList))
	for _, metadata := range metadataList {
		media := mediaMetadataToModel(metadata)
		results = append(results, &media)
	}
	return applyOffsetLimit(results, options), nil
}

func (self *fileSystemTransaction) createMedia(content io.Reader, metadata *models.Media, options *store.Option) (*models.Media, error) {
	if content == nil {
		return nil, store.ErrInvalidOptions
	}
	if metadata == nil {
		metadata = &models.Media{}
	}
	format := metadata.GetFormat()
	if format == "" {
		format = "bin"
	}

	mediaId := security.NewULID()
	shardDirectory := self.mediaShardDirectory(mediaId)
	if makeDirectoryError := os.MkdirAll(shardDirectory, 0755); makeDirectoryError != nil {
		return nil, fmt.Errorf("fsstore: creating media shard directory: %w", makeDirectoryError)
	}
	mediaPath := filepath.Join(shardDirectory, fmt.Sprintf("%s.%s", mediaId, format))
	mediaFile, createFileError := atomicfile.Create(mediaPath)
	if createFileError != nil {
		return nil, fmt.Errorf("fsstore: creating media file: %w", createFileError)
	}
	defer func() {
		_ = atomicfile.Discard(mediaFile)
	}()
	sizeBytes, copyError := io.Copy(mediaFile, content)
	if copyError != nil {
		return nil, fmt.Errorf("fsstore: writing media file: %w", copyError)
	}
	if commitError := atomicfile.Commit(mediaFile); commitError != nil {
		return nil, fmt.Errorf("fsstore: committing media file: %w", commitError)
	}
	metadataRecord := storeMediaMetadata{
		MediaID:        mediaId,
		UserID:         metadata.GetUserID(),
		Format:         format,
		SizeBytes:      sizeBytes,
		CreatedAt:      time.Now().UnixMilli(),
		SourceType:     string(metadata.GetSource()),
		AgentID:        metadata.GetSourceAgentID(),
		ConversationID: metadata.GetConversationID(),
		ToolName:       metadata.GetToolName(),
		ToolCallID:     metadata.GetToolCallID(),
		OriginalName:   metadata.GetOriginalName(),
	}
	if writeError := self.writeMediaMetadata(mediaId, metadataRecord); writeError != nil {
		_ = os.Remove(mediaPath)
		return nil, fmt.Errorf("fsstore: writing media metadata: %w", writeError)
	}
	result := mediaMetadataToModel(metadataRecord)
	return &result, nil
}

func (self *fileSystemTransaction) getMedia(mediaId string, options *store.Option) ([]byte, *models.Media, error) {
	mediaPath, format, err := self.findMediaFile(mediaId)
	if err != nil {
		return nil, nil, err
	}
	data, readError := os.ReadFile(mediaPath)
	if readError != nil {
		return nil, nil, readError
	}
	metadata, metadataError := self.loadOrSynthesizeMediaMetadata(mediaId, format, mediaPath)
	if metadataError != nil {
		return nil, nil, metadataError
	}
	result := mediaMetadataToModel(metadata)
	return data, &result, nil
}

func (self *fileSystemTransaction) openMedia(mediaId string, options *store.Option) (io.ReadCloser, *models.Media, error) {
	mediaPath, format, err := self.findMediaFile(mediaId)
	if err != nil {
		return nil, nil, err
	}
	mediaFile, openError := os.Open(mediaPath)
	if openError != nil {
		return nil, nil, openError
	}
	metadata, metadataError := self.loadOrSynthesizeMediaMetadata(mediaId, format, mediaPath)
	if metadataError != nil {
		_ = mediaFile.Close()
		return nil, nil, metadataError
	}
	result := mediaMetadataToModel(metadata)
	return mediaFile, &result, nil
}

func (self *fileSystemTransaction) modifyMedia(ctx context.Context, mediaId string, modifier func(*models.Media) error, options *store.Option) (*models.Media, error) {
	_, metadata, err := self.GetMedia(ctx, mediaId, options)
	if err != nil {
		return nil, err
	}
	if err := modifier(metadata); err != nil {
		return nil, err
	}
	updatedRecord := storeMediaMetadata{
		MediaID:        mediaId,
		UserID:         metadata.GetUserID(),
		Format:         metadata.GetFormat(),
		SizeBytes:      metadata.GetSize(),
		CreatedAt:      metadata.GetCreatedAt().UnixMilli(),
		SourceType:     string(metadata.GetSource()),
		AgentID:        metadata.GetSourceAgentID(),
		ConversationID: metadata.GetConversationID(),
		ToolName:       metadata.GetToolName(),
		ToolCallID:     metadata.GetToolCallID(),
		OriginalName:   metadata.GetOriginalName(),
	}
	if writeError := self.writeMediaMetadata(mediaId, updatedRecord); writeError != nil {
		return nil, fmt.Errorf("fsstore: writing media metadata: %w", writeError)
	}
	return metadata, nil
}

func (self *fileSystemTransaction) deleteMedia(mediaId string, options *store.Option) error {
	mediaPath, _, err := self.findMediaFile(mediaId)
	if err != nil {
		return err
	}
	if _, statError := os.Stat(mediaPath); statError == nil {
		if moveError := trash.Move(mediaPath, self.trashDirectory()); moveError != nil {
			return moveError
		}
	}
	metadataPath := self.mediaMetadataPath(mediaId)
	if _, statError := os.Stat(metadataPath); statError == nil {
		if moveError := trash.Move(metadataPath, self.trashDirectory()); moveError != nil {
			return moveError
		}
	}
	return nil
}

func (self *fileSystemTransaction) mediaShardDirectory(mediaId string) string {
	if len(mediaId) < 2 {
		return self.mediaDirectory()
	}
	return filepath.Join(self.mediaDirectory(), mediaId[len(mediaId)-2:])
}

func (self *fileSystemTransaction) mediaMetadataPath(mediaId string) string {
	return filepath.Join(self.mediaShardDirectory(mediaId), mediaId+mediaMetadataSuffix)
}

func (self *fileSystemTransaction) findMediaFile(mediaId string) (string, string, error) {
	matches, globError := filepath.Glob(filepath.Join(self.mediaShardDirectory(mediaId), mediaId+".*"))
	if globError != nil {
		return "", "", globError
	}
	for _, match := range matches {
		if strings.HasSuffix(match, mediaMetadataSuffix) {
			continue
		}
		format := strings.TrimPrefix(filepath.Ext(match), ".")
		return match, format, nil
	}
	return "", "", store.ErrNotFound
}

func (self *fileSystemTransaction) readMediaMetadata(mediaId string) (storeMediaMetadata, error) {
	data, readError := os.ReadFile(self.mediaMetadataPath(mediaId))
	if readError != nil {
		return storeMediaMetadata{}, readError
	}
	metadata := storeMediaMetadata{}
	if unmarshalError := json.Unmarshal(data, &metadata); unmarshalError != nil {
		return storeMediaMetadata{}, unmarshalError
	}
	return metadata, nil
}

func (self *fileSystemTransaction) writeMediaMetadata(mediaId string, metadata storeMediaMetadata) error {
	encoded, marshalError := json.Marshal(metadata)
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFile(self.mediaMetadataPath(mediaId), encoded)
}

func (self *fileSystemTransaction) loadOrSynthesizeMediaMetadata(mediaId string, format string, mediaPath string) (storeMediaMetadata, error) {
	metadata, metadataError := self.readMediaMetadata(mediaId)
	if metadataError == nil {
		return metadata, nil
	}
	if !os.IsNotExist(metadataError) {
		return storeMediaMetadata{}, metadataError
	}
	fileInfo, statError := os.Stat(mediaPath)
	if statError != nil {
		return storeMediaMetadata{}, statError
	}
	metadata = storeMediaMetadata{
		MediaID:   mediaId,
		Format:    format,
		SizeBytes: fileInfo.Size(),
		CreatedAt: fileInfo.ModTime().UnixMilli(),
	}
	_ = self.writeMediaMetadata(mediaId, metadata)
	return metadata, nil
}

func (self *fileSystemTransaction) scanMediaMetadata(filter func(storeMediaMetadata) bool) ([]storeMediaMetadata, error) {
	entries, readError := os.ReadDir(self.mediaDirectory())
	if readError != nil {
		if os.IsNotExist(readError) {
			return []storeMediaMetadata{}, nil
		}
		return nil, readError
	}
	mediaById := map[string]struct {
		format    string
		directory string
	}{}
	metadataEntries := make([]struct {
		mediaId   string
		directory string
	}, 0)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		shardDirectory := filepath.Join(self.mediaDirectory(), entry.Name())
		shardEntries, shardReadError := os.ReadDir(shardDirectory)
		if shardReadError != nil {
			continue
		}
		for _, shardEntry := range shardEntries {
			if shardEntry.IsDir() {
				continue
			}
			name := shardEntry.Name()
			if strings.HasSuffix(name, mediaMetadataSuffix) {
				metadataEntries = append(metadataEntries, struct {
					mediaId   string
					directory string
				}{
					mediaId:   strings.TrimSuffix(name, mediaMetadataSuffix),
					directory: shardDirectory,
				})
				continue
			}
			extension := filepath.Ext(name)
			baseName := strings.TrimSuffix(name, extension)
			format := strings.TrimPrefix(extension, ".")
			if baseName == "" || format == "" {
				continue
			}
			mediaById[baseName] = struct {
				format    string
				directory string
			}{format: format, directory: shardDirectory}
		}
	}

	results := make([]storeMediaMetadata, 0)
	for _, metadataEntry := range metadataEntries {
		if _, exists := mediaById[metadataEntry.mediaId]; !exists {
			_ = trash.Move(filepath.Join(metadataEntry.directory, metadataEntry.mediaId+mediaMetadataSuffix), self.trashDirectory())
			continue
		}
		metadataPath := filepath.Join(metadataEntry.directory, metadataEntry.mediaId+mediaMetadataSuffix)
		data, metadataReadError := os.ReadFile(metadataPath)
		if metadataReadError != nil {
			continue
		}
		metadata := storeMediaMetadata{}
		if unmarshalError := json.Unmarshal(data, &metadata); unmarshalError != nil {
			continue
		}
		if filter == nil || filter(metadata) {
			results = append(results, metadata)
		}
		delete(mediaById, metadataEntry.mediaId)
	}
	return results, nil
}

func mediaMetadataToModel(metadata storeMediaMetadata) models.Media {
	format := metadata.Format
	createdAt := time.UnixMilli(metadata.CreatedAt)
	modifiedAt := createdAt
	size := metadata.SizeBytes
	contentType := mimetypes.MIMETypeFromFormat(metadata.Format)
	return models.Media{
		ID:             metadata.MediaID,
		UserID:         ptrto.TrimmedString(metadata.UserID),
		Format:         &format,
		ContentType:    &contentType,
		Source:         ptrto.Trimmed[models.MediaSource](metadata.SourceType),
		SourceAgentID:  ptrto.TrimmedString(metadata.AgentID),
		ConversationID: ptrto.TrimmedString(metadata.ConversationID),
		ToolName:       ptrto.TrimmedString(metadata.ToolName),
		ToolCallID:     ptrto.TrimmedString(metadata.ToolCallID),
		OriginalName:   ptrto.TrimmedString(metadata.OriginalName),
		Size:           &size,
		CreatedAt:      &createdAt,
		ModifiedAt:     &modifiedAt,
	}
}
