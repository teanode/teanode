package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func (self *fileSystemTransaction) ListConversationMessages(ctx context.Context, conversationId string, options *store.Option) ([]*models.ConversationMessage, error) {
	return self.listConversationMessages(ctx, conversationId, options)
}

func (self *fileSystemTransaction) CreateConversationMessage(ctx context.Context, message *models.ConversationMessage, options *store.Option) (*models.ConversationMessage, error) {
	return self.createConversationMessage(ctx, message, options)
}

func (self *fileSystemTransaction) GetConversationMessage(ctx context.Context, messageId string, options *store.Option) (*models.ConversationMessage, error) {
	return self.getConversationMessage(ctx, messageId, options)
}

func (self *fileSystemTransaction) ModifyConversationMessage(ctx context.Context, messageId string, modifier func(*models.ConversationMessage) error, options *store.Option) (*models.ConversationMessage, error) {
	return self.modifyConversationMessage(ctx, messageId, modifier, options)
}

func (self *fileSystemTransaction) DeleteConversationMessage(ctx context.Context, messageId string, options *store.Option) error {
	return self.deleteConversationMessage(ctx, messageId, options)
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
	for index, rawMessage := range messages {
		sequence := int64(index + 1)
		convertedMessage := conversationRawMessageToModel(conversationId, sequence, rawMessage)
		results = append(results, &convertedMessage)
	}
	return applyOffsetLimitConversationMessages(results, options), nil
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
	timestamp := time.Now().UnixMilli()
	appendMessage := conversationFileMessage{
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

	messages, listError := self.ListConversationMessages(ctx, *message.ConversationID, nil)
	if listError != nil || len(messages) == 0 {
		return message, listError
	}
	return messages[len(messages)-1], nil
}

func (self *fileSystemTransaction) getConversationMessage(ctx context.Context, messageId string, options *store.Option) (*models.ConversationMessage, error) {
	conversationId, sequence, parseError := parseConversationMessageId(messageId)
	if parseError != nil {
		return nil, store.ErrNotFound
	}
	messages, err := self.ListConversationMessages(ctx, conversationId, nil)
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		if message.Sequence != nil && *message.Sequence == sequence {
			return message, nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) modifyConversationMessage(ctx context.Context, messageId string, modifier func(*models.ConversationMessage) error, options *store.Option) (*models.ConversationMessage, error) {
	conversationId, sequence, parseError := parseConversationMessageId(messageId)
	if parseError != nil {
		return nil, store.ErrNotFound
	}
	conversation, err := self.GetConversation(ctx, conversationId, options)
	if err != nil {
		return nil, err
	}
	header, rawMessages, loadError := self.loadConversationData(*conversation.UserID, *conversation.AgentID, conversationId)
	if loadError != nil {
		return nil, loadError
	}
	messageIndex := int(sequence - 1)
	if messageIndex < 0 || messageIndex >= len(rawMessages) {
		return nil, store.ErrNotFound
	}
	message := conversationRawMessageToModel(conversationId, sequence, rawMessages[messageIndex])
	if modifierError := modifier(&message); modifierError != nil {
		return nil, modifierError
	}

	updatedRawMessage := rawMessages[messageIndex]
	if message.Role != nil {
		updatedRawMessage.Role = string(*message.Role)
	}
	if len(message.Content) > 0 {
		updatedRawMessage.Content = json.RawMessage(message.Content)
	}
	updatedRawMessage.ToolCalls = json.RawMessage(message.ToolCalls)
	updatedRawMessage.Usage = json.RawMessage(message.Usage)
	updatedRawMessage.Metadata = json.RawMessage(message.Metadata)
	updatedRawMessage.StopReason = string(message.GetStopReason())
	updatedRawMessage.Model = message.GetModel()
	updatedRawMessage.Provider = message.GetProvider()
	updatedRawMessage.ToolCallID = message.GetToolCallID()
	updatedRawMessage.ToolName = message.GetToolName()
	rawMessages[messageIndex] = updatedRawMessage

	if rewriteError := self.rewriteConversationFile(*conversation.UserID, *conversation.AgentID, conversationId, header, rawMessages); rewriteError != nil {
		return nil, rewriteError
	}
	return self.GetConversationMessage(ctx, messageId, options)
}

func (self *fileSystemTransaction) deleteConversationMessage(ctx context.Context, messageId string, options *store.Option) error {
	conversationId, sequence, parseError := parseConversationMessageId(messageId)
	if parseError != nil {
		return store.ErrNotFound
	}
	conversation, err := self.GetConversation(ctx, conversationId, options)
	if err != nil {
		return err
	}
	header, rawMessages, loadError := self.loadConversationData(*conversation.UserID, *conversation.AgentID, conversationId)
	if loadError != nil {
		return loadError
	}
	messageIndex := int(sequence - 1)
	if messageIndex < 0 || messageIndex >= len(rawMessages) {
		return store.ErrNotFound
	}
	remainingMessages := append(rawMessages[:messageIndex], rawMessages[messageIndex+1:]...)
	return self.rewriteConversationFile(*conversation.UserID, *conversation.AgentID, conversationId, header, remainingMessages)
}

func conversationRawMessageToModel(conversationId string, sequence int64, message conversationFileMessage) models.ConversationMessage {
	messageId := fmt.Sprintf("%s:%d", conversationId, sequence)
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
		Sequence:       &sequence,
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

func applyOffsetLimitConversationMessages(values []*models.ConversationMessage, options *store.Option) []*models.ConversationMessage {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []*models.ConversationMessage{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}

func stopReasonPointer(value string) *models.StopReason {
	trimmedValue := value
	if trimmedValue == "" {
		return nil
	}
	stopReason := models.StopReason(trimmedValue)
	return &stopReason
}

func parseConversationMessageId(messageId string) (string, int64, error) {
	parts := strings.Split(messageId, ":")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid message id")
	}
	sequence, parseError := strconv.ParseInt(parts[1], 10, 64)
	if parseError != nil {
		return "", 0, parseError
	}
	return parts[0], sequence, nil
}
