package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func (self *fileSystemTransaction) ListConversationMessages(ctx context.Context, conversationId string, options *store.Option) ([]*models.ConversationMessage, error) {
	return self.listConversationMessages(ctx, conversationId, options)
}

func (self *fileSystemTransaction) CreateConversationMessage(ctx context.Context, message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	return self.createConversationMessage(ctx, message, options)
}


func (self *fileSystemTransaction) listConversationMessages(ctx context.Context, conversationId string, options *store.Option) ([]*models.ConversationMessage, error) {
	conversation, err := self.GetConversation(ctx, conversationId, options)
	if err != nil {
		return nil, err
	}
	_, messages, loadError := self.loadConversationData(*conversation.UserID, *conversation.AgentID, conversationId)
	if loadError != nil {
		return nil, loadError
	}
	results := make([]*models.ConversationMessage, 0, len(messages))
	for _, rawMessage := range messages {
		convertedMessage := conversationRawMessageToModel(conversationId, rawMessage)
		results = append(results, &convertedMessage)
	}
	return applyOffsetLimit(results, options), nil
}

func (self *fileSystemTransaction) createConversationMessage(ctx context.Context, message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	if message == nil || message.ConversationID == nil || message.Role == nil || len(message.Content) == 0 {
		return nil, store.ErrInvalidOptions
	}
	conversation, err := self.GetConversation(ctx, *message.ConversationID, options)
	if err != nil {
		return nil, err
	}
	conversationPath := self.conversationFilePath(*conversation.UserID, *conversation.AgentID, *message.ConversationID)
	if _, statError := os.Stat(conversationPath); statError != nil {
		if os.IsNotExist(statError) {
			return nil, store.ErrNotFound
		}
		return nil, statError
	}
	messageId := security.NewULID()
	timestamp := time.Now().UnixMilli()
	appendMessage := conversationFileMessage{
		ID:         messageId,
		Role:       string(*message.Role),
		Content:    json.RawMessage(message.Content),
		Timestamp:  timestamp,
		ToolCalls:  json.RawMessage(message.ToolCalls),
		Usage:      json.RawMessage(message.Usage),
		Metadata:   json.RawMessage(message.Metadata),
		StopReason: string(message.GetStopReason()),
		Model:      message.GetModel(),
		Provider:   message.GetProvider(),
		ToolCallID: message.GetToolCallID(),
		ToolName:   message.GetToolName(),
	}
	encodedMessage, marshalError := json.Marshal(appendMessage)
	if marshalError != nil {
		return nil, marshalError
	}
	file, openError := os.OpenFile(conversationPath, os.O_APPEND|os.O_WRONLY, 0644)
	if openError != nil {
		return nil, openError
	}
	defer file.Close()
	if _, writeError := file.Write(append(encodedMessage, '\n')); writeError != nil {
		return nil, writeError
	}
	if syncError := file.Sync(); syncError != nil {
		return nil, syncError
	}

	result := conversationRawMessageToModel(*message.ConversationID, appendMessage)
	return &result, nil
}


func conversationRawMessageToModel(conversationId string, message conversationFileMessage) models.ConversationMessage {
	messageId := message.ID
	if messageId == "" {
		// Legacy messages without an ID: generate a deterministic placeholder.
		messageId = fmt.Sprintf("%s:%s:%d", conversationId, message.Role, message.Timestamp)
	}
	role := models.Role(message.Role)
	createdAt := time.UnixMilli(message.Timestamp)
	toolCalls, usage, metadata := migrateMetadataEnvelope(message)
	return models.ConversationMessage{
		ID:             messageId,
		ConversationID: &conversationId,
		Role:           &role,
		Content:        json.RawMessage(message.Content),
		ToolCalls:      toolCalls,
		Usage:          usage,
		Metadata:       metadata,
		ToolCallID:     ptrto.TrimmedString(message.ToolCallID),
		ToolName:       ptrto.TrimmedString(message.ToolName),
		Model:          ptrto.TrimmedString(message.Model),
		Provider:       ptrto.TrimmedString(message.Provider),
		StopReason:     stopReasonPointer(message.StopReason),
		CreatedAt:      &createdAt,
		ModifiedAt:     &createdAt,
	}
}

// migrateMetadataEnvelope returns the three separate fields (toolCalls, usage,
// metadata) from a raw file message. It handles both the new format (fields are
// top-level on the file message) and the legacy envelope format (all three
// packed inside the Metadata field as a JSON object with keys "toolCalls",
// "usage", and "metadata").
func migrateMetadataEnvelope(message conversationFileMessage) (toolCalls, usage, metadata json.RawMessage) {
	toolCalls = json.RawMessage(message.ToolCalls)
	usage = json.RawMessage(message.Usage)
	metadata = nil

	// If top-level fields are already populated, use them directly.
	if len(toolCalls) > 0 || len(usage) > 0 {
		metadata = json.RawMessage(message.Metadata)
		return
	}

	// No top-level fields — check if Metadata contains a legacy envelope.
	if len(message.Metadata) == 0 {
		return
	}

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(message.Metadata, &envelope); err != nil {
		// Not a JSON object — treat as plain metadata.
		metadata = json.RawMessage(message.Metadata)
		return
	}

	// Extract envelope keys into separate fields.
	if raw, ok := envelope["toolCalls"]; ok && len(raw) > 0 {
		toolCalls = raw
	}
	if raw, ok := envelope["usage"]; ok && len(raw) > 0 {
		usage = raw
	}
	if raw, ok := envelope["metadata"]; ok && len(raw) > 0 {
		metadata = raw
	}

	return
}

func stopReasonPointer(value string) *models.StopReason {
	trimmedValue := value
	if trimmedValue == "" {
		return nil
	}
	stopReason := models.StopReason(trimmedValue)
	return &stopReason
}
