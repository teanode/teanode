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

func (self *memoryTool) executeSummary(ctx context.Context, scope models.Scope, scopeId string, args executeArguments) (string, error) {
	runner := runners.RunnerFromContext(ctx)
	if runner == nil || runner.ConversationID == "" {
		return "", fmt.Errorf("no active conversation (conversationId not available)")
	}
	conversationId := runner.ConversationID

	// Fetch messages.
	var messages []*models.ConversationMessage
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		messages, err = tx.ListConversationMessages(ctx, conversationId, nil)
		return err
	}); err != nil {
		return "", err
	}

	// Apply roles filter.
	if len(args.Roles) > 0 {
		roleSet := make(map[string]bool, len(args.Roles))
		for _, role := range args.Roles {
			roleSet[role] = true
		}
		var filtered []*models.ConversationMessage
		for _, message := range messages {
			if roleSet[string(message.GetRole())] {
				filtered = append(filtered, message)
			}
		}
		messages = filtered
	}

	// Apply maxMessages (take last N).
	if args.MaxMessages > 0 && len(messages) > args.MaxMessages {
		messages = messages[len(messages)-args.MaxMessages:]
	}

	// Count by role.
	byRole := map[string]int{
		"user":      0,
		"assistant": 0,
		"system":    0,
		"tool":      0,
	}
	for _, message := range messages {
		role := string(message.GetRole())
		byRole[role]++
	}

	// Time range.
	var firstAt, lastAt *time.Time
	if len(messages) > 0 {
		firstAt = messages[0].CreatedAt
		lastAt = messages[len(messages)-1].CreatedAt
	}

	// Topic segmentation.
	topicSegments := buildTopicSegments(messages)

	// Key points: first sentence of each assistant message that immediately follows a user message.
	keyPoints := extractKeyPoints(messages)

	// Build summary output.
	summaryMap := map[string]interface{}{
		"totalMessages": len(messages),
		"byRole":        byRole,
	}
	if firstAt != nil {
		summaryMap["firstMessageAt"] = firstAt.Format(time.RFC3339)
	}
	if lastAt != nil {
		summaryMap["lastMessageAt"] = lastAt.Format(time.RFC3339)
	}
	if len(topicSegments) > 0 {
		summaryMap["topicSegments"] = topicSegments
	}
	if len(keyPoints) > 0 {
		summaryMap["keyPoints"] = keyPoints
	}

	result := map[string]interface{}{
		"action":         "summary",
		"conversationId": conversationId,
		"messageCount":   len(messages),
		"summary":        summaryMap,
	}

	// Persist if requested.
	if args.Persist != nil {
		title := "Conversation summary"
		if args.Persist.Title != "" {
			title = args.Persist.Title
		}
		summaryJSON, _ := json.MarshalIndent(summaryMap, "", "  ")
		content := string(summaryJSON)
		if len(content) > maxContentSize {
			return "", fmt.Errorf("summary content exceeds maximum size of %d bytes", maxContentSize)
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

type topicSegment struct {
	StartIndex   int    `json:"startIndex"`
	EndIndex     int    `json:"endIndex"`
	Topic        string `json:"topic"`
	MessageCount int    `json:"messageCount"`
}

func buildTopicSegments(messages []*models.ConversationMessage) []topicSegment {
	if len(messages) == 0 {
		return nil
	}

	// Extract significant tokens (>4 chars) from user messages.
	type messageTokens struct {
		index  int
		tokens map[string]int
	}
	var userMessages []messageTokens
	for i, message := range messages {
		if message.GetRole() == models.RoleUser {
			text := extractTextContent(message.Content)
			tokens := significantTokens(text)
			userMessages = append(userMessages, messageTokens{index: i, tokens: tokens})
		}
	}

	if len(userMessages) == 0 {
		return []topicSegment{{
			StartIndex:   0,
			EndIndex:     len(messages) - 1,
			Topic:        "conversation",
			MessageCount: len(messages),
		}}
	}

	// Slide a window of 5 user messages. When a user message shares no
	// significant tokens with the previous window, start a new segment.
	type segmentRange struct {
		startMessageIndex int
		endMessageIndex   int
		tokenFrequency    map[string]int
	}
	windowSize := 5
	segments := []segmentRange{{
		startMessageIndex: 0,
		endMessageIndex:   userMessages[0].index,
		tokenFrequency:    copyTokens(userMessages[0].tokens),
	}}

	for userIndex := 1; userIndex < len(userMessages); userIndex++ {
		current := userMessages[userIndex]
		segment := &segments[len(segments)-1]

		// Build window tokens from last `windowSize` user messages in this segment.
		windowStart := userIndex - windowSize
		if windowStart < 0 {
			windowStart = 0
		}
		windowTokens := map[string]bool{}
		for windowIndex := windowStart; windowIndex < userIndex; windowIndex++ {
			for token := range userMessages[windowIndex].tokens {
				windowTokens[token] = true
			}
		}

		// Check overlap.
		overlap := false
		for token := range current.tokens {
			if windowTokens[token] {
				overlap = true
				break
			}
		}

		if overlap {
			// Extend current segment.
			segment.endMessageIndex = current.index
			for token, count := range current.tokens {
				segment.tokenFrequency[token] += count
			}
		} else {
			// Start new segment.
			segments = append(segments, segmentRange{
				startMessageIndex: current.index,
				endMessageIndex:   current.index,
				tokenFrequency:    copyTokens(current.tokens),
			})
		}
	}

	// Extend last segment to cover remaining messages.
	segments[len(segments)-1].endMessageIndex = len(messages) - 1

	// Build output.
	result := make([]topicSegment, len(segments))
	for i, segment := range segments {
		topic := mostFrequentToken(segment.tokenFrequency)
		if topic == "" {
			topic = "conversation"
		}
		result[i] = topicSegment{
			StartIndex:   segment.startMessageIndex,
			EndIndex:     segment.endMessageIndex,
			Topic:        topic,
			MessageCount: segment.endMessageIndex - segment.startMessageIndex + 1,
		}
	}
	return result
}

func significantTokens(text string) map[string]int {
	frequency := map[string]int{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		// Strip common punctuation.
		word = strings.Trim(word, ".,;:!?\"'()[]{}#*")
		if len(word) > 4 {
			frequency[word]++
		}
	}
	return frequency
}

func mostFrequentToken(frequency map[string]int) string {
	best := ""
	bestCount := 0
	for token, count := range frequency {
		if count > bestCount || (count == bestCount && token < best) {
			best = token
			bestCount = count
		}
	}
	return best
}

func copyTokens(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for token, count := range source {
		result[token] = count
	}
	return result
}

func extractKeyPoints(messages []*models.ConversationMessage) []string {
	seen := map[string]bool{}
	var points []string
	for i := 1; i < len(messages); i++ {
		if messages[i].GetRole() != models.RoleAssistant {
			continue
		}
		if messages[i-1].GetRole() != models.RoleUser {
			continue
		}
		text := extractTextContent(messages[i].Content)
		sentence := firstSentence(text)
		if sentence == "" {
			continue
		}
		if seen[sentence] {
			continue
		}
		seen[sentence] = true
		points = append(points, sentence)
		if len(points) >= 10 {
			break
		}
	}
	return points
}

func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	// Find first sentence-ending punctuation.
	for i, character := range text {
		if character == '.' || character == '!' || character == '?' {
			return strings.TrimSpace(text[:i+1])
		}
		// Cap at 200 chars if no sentence-ending punctuation found.
		if i >= 200 {
			return strings.TrimSpace(text[:i])
		}
	}
	return text
}
