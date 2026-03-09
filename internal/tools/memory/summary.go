package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/summarizers"
)

const summaryMaxConversationCharacters = 8000
const summaryMaxMessageCharacters = 2000

// Synthesizer abstracts the LLM call used by the summary action so that tests
// can inject a deterministic stub.
type Synthesizer interface {
	Synthesize(ctx context.Context, systemPrompt string, userPrompt string) (string, error)
}

// defaultSynthesizer retrieves the Summarizer from the context and delegates
// to its RunSynthesis method.
type defaultSynthesizer struct{}

func (self *defaultSynthesizer) Synthesize(ctx context.Context, systemPrompt string, userPrompt string) (string, error) {
	summarizer := summarizers.SummarizerFromContext(ctx)
	if summarizer == nil {
		return "", fmt.Errorf("no summarizer available in context")
	}
	result, ok := summarizer.RunSynthesis(ctx, systemPrompt, userPrompt)
	if !ok {
		return "", fmt.Errorf("synthesis request failed")
	}
	return result, nil
}

// synthesizer is the package-level Synthesizer used by executeSummary. Tests
// replace this with a stub before invoking the tool.
var synthesizer Synthesizer = &defaultSynthesizer{}

// structuredSummaryResult mirrors the JSON schema returned by the LLM.
type structuredSummaryResult struct {
	Summary       string        `json:"summary"`
	CriticalFacts criticalFacts `json:"criticalFacts"`
}

type criticalFacts struct {
	Decisions       []string `json:"decisions"`
	Todos           []string `json:"todos"`
	Constraints     []string `json:"constraints"`
	UserPreferences []string `json:"userPreferences"`
	OpenQuestions   []string `json:"openQuestions"`
}

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

	messageCount := len(messages)

	// Build truncated transcript.
	chunkText := summarizers.BuildMessagesText(messages, summaryMaxConversationCharacters, summaryMaxMessageCharacters)

	// Call LLM to produce structured summary.
	userPrompt := prompts.BuildStructuredSummaryUserPrompt("", "", chunkText)
	responseText, err := synthesizer.Synthesize(ctx, prompts.StructuredSummarySystemPrompt, userPrompt)
	if err != nil {
		return "", fmt.Errorf("summary synthesis failed: %w", err)
	}

	// Parse the structured JSON response.
	var structured structuredSummaryResult
	if err := json.Unmarshal([]byte(responseText), &structured); err != nil {
		return "", fmt.Errorf("failed to parse summary response: %w", err)
	}

	result := map[string]interface{}{
		"action":         "summary",
		"conversationId": conversationId,
		"messageCount":   messageCount,
		"summary":        structured.Summary,
		"criticalFacts":  structured.CriticalFacts,
	}

	// Persist if requested.
	if args.Persist != nil {
		title := "Conversation summary"
		if args.Persist.Title != "" {
			title = args.Persist.Title
		}

		content := buildCompactSummaryContent(structured)
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
			addResult := self.batchAdd(ctx, tx, scope, scopeId, 0, persistItem, precomputedEmbedding{})
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

// buildCompactSummaryContent produces a compact JSON string containing the
// short summary and critical facts lists for persistence.
func buildCompactSummaryContent(structured structuredSummaryResult) string {
	compact, _ := json.Marshal(structured)
	return string(compact)
}
