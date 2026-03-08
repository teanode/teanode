package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
)

func (self *memoryTool) executeFilter(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	runner := runners.RunnerFromContext(ctx)
	if runner == nil || runner.ConversationID == "" {
		return "", fmt.Errorf("no active conversation (conversationId not available)")
	}
	conversationId := runner.ConversationID

	// Fetch all messages.
	var messages []*models.ConversationMessage
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		messages, err = tx.ListConversationMessages(ctx, conversationId, nil)
		return err
	}); err != nil {
		return "", err
	}

	// Apply filters.
	var filtered []*models.ConversationMessage

	// Roles filter.
	roleSet := map[string]bool{}
	if len(args.Roles) > 0 {
		for _, role := range args.Roles {
			roleSet[role] = true
		}
	}

	// Time filters.
	var afterTime, beforeTime *time.Time
	if args.After != "" {
		parsed, err := time.Parse(time.RFC3339, args.After)
		if err != nil {
			return "", fmt.Errorf("invalid 'after' time format: %w", err)
		}
		afterTime = &parsed
	}
	if args.Before != "" {
		parsed, err := time.Parse(time.RFC3339, args.Before)
		if err != nil {
			return "", fmt.Errorf("invalid 'before' time format: %w", err)
		}
		beforeTime = &parsed
	}

	keywordLower := strings.ToLower(args.Keyword)

	for _, message := range messages {
		// Role filter.
		if len(roleSet) > 0 && !roleSet[string(message.GetRole())] {
			continue
		}
		// After filter.
		if afterTime != nil && message.CreatedAt != nil && !message.CreatedAt.After(*afterTime) {
			continue
		}
		// Before filter.
		if beforeTime != nil && message.CreatedAt != nil && !message.CreatedAt.Before(*beforeTime) {
			continue
		}
		// Keyword filter.
		if keywordLower != "" {
			text := strings.ToLower(extractTextContent(message.Content))
			if !strings.Contains(text, keywordLower) {
				continue
			}
		}
		filtered = append(filtered, message)
	}

	totalMatched := len(filtered)

	// Truncate to maxResults (take most recent).
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if len(filtered) > maxResults {
		filtered = filtered[len(filtered)-maxResults:]
	}

	// Build output messages.
	type outputMessage struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		CreatedAt string `json:"createdAt,omitempty"`
	}
	outputMessages := make([]outputMessage, len(filtered))
	for i, message := range filtered {
		output := outputMessage{
			ID:      message.ID,
			Role:    string(message.GetRole()),
			Content: extractTextContent(message.Content),
		}
		if message.CreatedAt != nil {
			output.CreatedAt = message.CreatedAt.Format(time.RFC3339)
		}
		outputMessages[i] = output
	}

	result := map[string]interface{}{
		"action":         "filter",
		"conversationId": conversationId,
		"messages":       outputMessages,
		"totalMatched":   totalMatched,
	}

	// Persist if requested.
	if args.Persist != nil {
		title := "Filtered messages"
		if args.Persist.Title != "" {
			title = args.Persist.Title
		}
		contentJSON, _ := json.MarshalIndent(outputMessages, "", "  ")
		content := string(contentJSON)
		if len(content) > maxContentSize {
			return "", fmt.Errorf("filtered content exceeds maximum size of %d bytes", maxContentSize)
		}

		persistItem := batchItem{
			Op:      "add",
			Title:   title,
			Content: content,
			Tags:    args.Persist.Tags,
		}
		var itemId string
		if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
			addResult := self.batchAdd(ctx, tx, scope, scopeId, 0, persistItem)
			if !addResult.Success {
				return fmt.Errorf("persist failed: %s", addResult.Error)
			}
			if id, ok := addResult.Item["id"].(string); ok {
				itemId = id
			}
			return nil
		}); err != nil {
			return "", err
		}
		self.callAfterMutate(ctx, scopeId)
		result["persisted"] = map[string]interface{}{"itemId": itemId}
	}

	output, _ := json.Marshal(result)
	return string(output), nil
}
