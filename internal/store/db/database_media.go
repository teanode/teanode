package db

import (
	"errors"
	"gorm.io/gorm"
	"io"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

type databaseMediaRecord struct {
	ID             string    `gorm:"column:id;type:varchar(32);primaryKey"`
	UserID         *string   `gorm:"column:user_id;type:varchar(32)"`
	Format         *string   `gorm:"column:format;type:varchar(32)"`
	ContentType    *string   `gorm:"column:content_type;type:varchar(128)"`
	Source         *string   `gorm:"column:source;type:varchar(32)"`
	SourceAgentID  *string   `gorm:"column:source_agent_id;type:varchar(32)"`
	ConversationID *string   `gorm:"column:conversation_id;type:varchar(32)"`
	ToolName       *string   `gorm:"column:tool_name;type:varchar(128)"`
	ToolCallID     *string   `gorm:"column:tool_call_id;type:varchar(128)"`
	OriginalName   *string   `gorm:"column:original_name;type:varchar(256)"`
	Size           *int64    `gorm:"column:size"`
	LargeObjectID  uint32    `gorm:"column:large_object_id;type:oid;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;not null"`
	ModifiedAt     time.Time `gorm:"column:modified_at;not null"`
}

func (databaseMediaRecord) TableName() string {
	return "media"
}

func (self *databaseTransaction) ListMedia(listOptions store.MediaListOptions, options *store.Option) ([]models.Media, error) {
	query := self.database.Model(&databaseMediaRecord{})
	if listOptions.UserID != nil {
		query = query.Where("user_id = ?", strings.TrimSpace(*listOptions.UserID))
	}
	if listOptions.ConversationID != nil {
		query = query.Where("conversation_id = ?", strings.TrimSpace(*listOptions.ConversationID))
	}
	if listOptions.Source != nil {
		query = query.Where("source = ?", strings.TrimSpace(*listOptions.Source))
	}
	if listOptions.ToolName != nil {
		query = query.Where("tool_name = ?", strings.TrimSpace(*listOptions.ToolName))
	}
	query = applyOption(query.Order("created_at DESC"), options)
	records := make([]databaseMediaRecord, 0)
	listError := query.Find(&records).Error
	if listError != nil {
		return nil, databaseError(listError)
	}
	mediaItems := make([]models.Media, 0, len(records))
	for _, record := range records {
		mediaItems = append(mediaItems, *mediaRecordToModel(&record))
	}
	return mediaItems, nil
}

func (self *databaseTransaction) CreateMedia(content io.Reader, metadata *models.Media, options *store.Option) (*models.Media, error) {
	if content == nil {
		return nil, store.ErrInvalidOptions
	}
	if metadata == nil {
		metadata = &models.Media{}
	}
	record := modelToMediaRecord(metadata)
	if strings.TrimSpace(record.ID) == "" {
		record.ID = security.NewULID()
	}
	largeObjectId, sizeBytes, createLargeObjectError := self.createLargeObjectFromReader(content)
	if createLargeObjectError != nil {
		return nil, createLargeObjectError
	}
	record.LargeObjectID = largeObjectId
	record.Size = &sizeBytes
	record.CreatedAt = valueOrTime(metadata.CreatedAt)
	record.ModifiedAt = valueOrTime(metadata.ModifiedAt)
	createError := self.database.Create(record).Error
	if createError != nil {
		_ = self.deleteLargeObject(record.LargeObjectID)
		return nil, databaseError(createError)
	}
	_, createdMedia, getError := self.GetMedia(record.ID, options)
	if getError != nil {
		return nil, getError
	}
	return createdMedia, nil
}

func (self *databaseTransaction) GetMedia(mediaId string, options *store.Option) ([]byte, *models.Media, error) {
	record := &databaseMediaRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(mediaId)).Take(record).Error
	if getError != nil {
		return nil, nil, databaseError(getError)
	}
	content, readLargeObjectError := self.readLargeObjectBytes(record.LargeObjectID)
	if readLargeObjectError != nil {
		return nil, nil, readLargeObjectError
	}
	return content, mediaRecordToModel(record), nil
}

func (self *databaseTransaction) OpenMedia(mediaId string, options *store.Option) (io.ReadCloser, *models.Media, error) {
	record := &databaseMediaRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(mediaId)).Take(record).Error
	if getError != nil {
		return nil, nil, getError
	}
	databaseHandle := self.rootDatabaseHandle
	if databaseHandle == nil {
		databaseHandle = self.database
	}
	return &databaseLargeObjectReadCloser{
		databaseHandle: databaseHandle,
		largeObjectId:  record.LargeObjectID,
		chunkSizeBytes: 1024 * 1024,
	}, mediaRecordToModel(record), nil
}

func (self *databaseTransaction) ModifyMedia(mediaId string, modifier func(*models.Media) error, options *store.Option) (*models.Media, error) {
	existingRecord := &databaseMediaRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(mediaId)).Take(existingRecord).Error
	if getError != nil {
		return nil, databaseError(getError)
	}
	media := mediaRecordToModel(existingRecord)
	if modifierError := modifier(media); modifierError != nil {
		return nil, modifierError
	}
	record := modelToMediaRecord(media)
	record.ID = strings.TrimSpace(mediaId)
	record.LargeObjectID = existingRecord.LargeObjectID
	record.ModifiedAt = time.Now().UTC()
	if media.CreatedAt != nil {
		record.CreatedAt = media.CreatedAt.UTC()
	}
	updateError := self.database.Model(&databaseMediaRecord{}).Where("id = ?", record.ID).Updates(map[string]interface{}{
		"user_id":         record.UserID,
		"format":          record.Format,
		"content_type":    record.ContentType,
		"source":          record.Source,
		"source_agent_id": record.SourceAgentID,
		"conversation_id": record.ConversationID,
		"tool_name":       record.ToolName,
		"tool_call_id":    record.ToolCallID,
		"original_name":   record.OriginalName,
		"size":            record.Size,
		"large_object_id": record.LargeObjectID,
		"modified_at":     record.ModifiedAt,
	}).Error
	if updateError != nil {
		return nil, databaseError(updateError)
	}
	_, updatedMedia, updatedError := self.GetMedia(record.ID, options)
	return updatedMedia, updatedError
}

func (self *databaseTransaction) DeleteMedia(mediaId string, options *store.Option) error {
	record := &databaseMediaRecord{}
	getError := self.database.Where("id = ?", strings.TrimSpace(mediaId)).Take(record).Error
	if getError != nil {
		return databaseError(getError)
	}
	result := self.database.Where("id = ?", strings.TrimSpace(mediaId)).Delete(&databaseMediaRecord{})
	if result.Error != nil {
		return databaseError(result.Error)
	}
	if result.RowsAffected == 0 {
		return store.ErrNotFound
	}
	if deleteLargeObjectError := self.deleteLargeObject(record.LargeObjectID); deleteLargeObjectError != nil {
		return deleteLargeObjectError
	}
	return nil
}

func modelToMediaRecord(media *models.Media) *databaseMediaRecord {
	return &databaseMediaRecord{
		ID:             strings.TrimSpace(media.ID),
		UserID:         ptrto.TrimmedString(valueOrEmptyString(media.UserID)),
		Format:         ptrto.TrimmedString(valueOrEmptyString(media.Format)),
		ContentType:    ptrto.TrimmedString(valueOrEmptyString(media.ContentType)),
		Source:         ptrto.TrimmedString(valueOrEmptyString(media.Source)),
		SourceAgentID:  ptrto.TrimmedString(valueOrEmptyString(media.SourceAgentID)),
		ConversationID: ptrto.TrimmedString(valueOrEmptyString(media.ConversationID)),
		ToolName:       ptrto.TrimmedString(valueOrEmptyString(media.ToolName)),
		ToolCallID:     ptrto.TrimmedString(valueOrEmptyString(media.ToolCallID)),
		OriginalName:   ptrto.TrimmedString(valueOrEmptyString(media.OriginalName)),
		Size:           media.Size,
	}
}

func mediaRecordToModel(record *databaseMediaRecord) *models.Media {
	return &models.Media{
		ID:             record.ID,
		UserID:         ptrto.TrimmedString(valueOrEmptyString(record.UserID)),
		Format:         ptrto.TrimmedString(valueOrEmptyString(record.Format)),
		ContentType:    ptrto.TrimmedString(valueOrEmptyString(record.ContentType)),
		Source:         ptrto.TrimmedString(valueOrEmptyString(record.Source)),
		SourceAgentID:  ptrto.TrimmedString(valueOrEmptyString(record.SourceAgentID)),
		ConversationID: ptrto.TrimmedString(valueOrEmptyString(record.ConversationID)),
		ToolName:       ptrto.TrimmedString(valueOrEmptyString(record.ToolName)),
		ToolCallID:     ptrto.TrimmedString(valueOrEmptyString(record.ToolCallID)),
		OriginalName:   ptrto.TrimmedString(valueOrEmptyString(record.OriginalName)),
		Size:           record.Size,
		CreatedAt:      &record.CreatedAt,
		ModifiedAt:     &record.ModifiedAt,
	}
}

func (self *databaseTransaction) createLargeObjectFromReader(content io.Reader) (uint32, int64, error) {
	var largeObjectId uint32
	createError := self.database.Raw("SELECT lo_create(0) AS oid").Scan(&largeObjectId).Error
	if createError != nil {
		return 0, 0, databaseError(createError)
	}
	offset := int64(0)
	chunkBuffer := make([]byte, 1024*1024)
	for {
		readCount, readError := content.Read(chunkBuffer)
		if readCount > 0 {
			writeChunk := chunkBuffer[:readCount]
			writeError := self.database.Exec("SELECT lo_put(?, ?, ?)", largeObjectId, offset, writeChunk).Error
			if writeError != nil {
				_ = self.deleteLargeObject(largeObjectId)
				return 0, 0, databaseError(writeError)
			}
			offset += int64(readCount)
		}
		if errors.Is(readError, io.EOF) {
			break
		}
		if readError != nil {
			_ = self.deleteLargeObject(largeObjectId)
			return 0, 0, readError
		}
	}
	return largeObjectId, offset, nil
}

func (self *databaseTransaction) readLargeObjectBytes(largeObjectId uint32) ([]byte, error) {
	content := make([]byte, 0)
	readError := self.database.Raw("SELECT lo_get(?)", largeObjectId).Scan(&content).Error
	if readError != nil {
		return nil, databaseError(readError)
	}
	return content, nil
}

func (self *databaseTransaction) deleteLargeObject(largeObjectId uint32) error {
	deleteError := self.database.Exec("SELECT lo_unlink(?)", largeObjectId).Error
	if deleteError != nil {
		return databaseError(deleteError)
	}
	return nil
}

type databaseLargeObjectReadCloser struct {
	databaseHandle *gorm.DB
	largeObjectId  uint32
	chunkSizeBytes int64
	offsetBytes    int64
	pendingChunk   []byte
	reachedEOF     bool
	closed         bool
}

func (self *databaseLargeObjectReadCloser) Read(content []byte) (int, error) {
	if self.closed {
		return 0, io.EOF
	}
	for len(self.pendingChunk) == 0 && !self.reachedEOF {
		chunk, readError := self.readNextChunk()
		if readError != nil {
			return 0, readError
		}
		if len(chunk) == 0 {
			self.reachedEOF = true
			break
		}
		self.pendingChunk = chunk
	}
	if len(self.pendingChunk) == 0 && self.reachedEOF {
		return 0, io.EOF
	}
	readCount := copy(content, self.pendingChunk)
	self.pendingChunk = self.pendingChunk[readCount:]
	return readCount, nil
}

func (self *databaseLargeObjectReadCloser) Close() error {
	self.closed = true
	self.pendingChunk = nil
	return nil
}

func (self *databaseLargeObjectReadCloser) readNextChunk() ([]byte, error) {
	chunk := make([]byte, 0)
	readError := self.databaseHandle.Raw(
		"SELECT lo_get(?, ?, ?)",
		self.largeObjectId,
		self.offsetBytes,
		self.chunkSizeBytes,
	).Scan(&chunk).Error
	if readError != nil {
		return nil, readError
	}
	self.offsetBytes += int64(len(chunk))
	return chunk, nil
}
