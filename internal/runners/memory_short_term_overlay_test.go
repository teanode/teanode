package runners

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
)

func ptrRole(r models.Role) *models.Role { return &r }
func ptrString(s string) *string         { return &s }

func makeMessage(role models.Role, content string) *models.ConversationMessage {
	raw, _ := json.Marshal(content)
	return &models.ConversationMessage{
		Role:    ptrRole(role),
		Content: raw,
	}
}

func makeToolMessage(toolName, content string) *models.ConversationMessage {
	msg := makeMessage(models.RoleTool, content)
	msg.ToolName = ptrString(toolName)
	msg.ToolCallID = ptrString("call_" + toolName)
	return msg
}

func retrieveResult(snippets ...map[string]any) string {
	result := map[string]any{
		"action":   "retrieve",
		"snippets": snippets,
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func snippet(title, text string, tags ...string) map[string]any {
	s := map[string]any{
		"title":   title,
		"snippet": text,
	}
	if len(tags) > 0 {
		s["tags"] = tags
	}
	return s
}

var testOptions = shortTermMemoryOverlayOptions{
	TurnTTL:         3,
	MaxItemsPerTool: 5,
	MaxCharsPerItem: 400,
	MaxCharsTotal:   4000,
}

func TestBasicRetrieveWithinTTL(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "retrieve my memories"),
		makeMessage(models.RoleAssistant, "Let me look that up."),
		makeToolMessage("user_memory", retrieveResult(snippet("Favorite color", "blue"))),
		makeMessage(models.RoleAssistant, "Your favorite color is blue."),
		makeMessage(models.RoleUser, "thanks"),
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	if result == "" {
		t.Fatal("expected overlay, got empty string")
	}
	if !strings.Contains(result, "user_memory.retrieve") {
		t.Error("expected user_memory.retrieve section")
	}
	if !strings.Contains(result, "Favorite color") {
		t.Error("expected snippet title")
	}
	if !strings.Contains(result, "blue") {
		t.Error("expected snippet content")
	}
}

func TestExpiredBeyondTTL(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "retrieve"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", retrieveResult(snippet("Old", "data"))),
		makeMessage(models.RoleAssistant, "here"),
		makeMessage(models.RoleUser, "turn 1"),
		makeMessage(models.RoleAssistant, "reply 1"),
		makeMessage(models.RoleUser, "turn 2"),
		makeMessage(models.RoleAssistant, "reply 2"),
		makeMessage(models.RoleUser, "turn 3"),
		makeMessage(models.RoleAssistant, "reply 3"),
		makeMessage(models.RoleUser, "turn 4"), // 4th user turn after retrieve
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	if result != "" {
		t.Errorf("expected empty overlay for expired results, got: %s", result)
	}
}

func TestMultipleTools(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "get memories"),
		makeMessage(models.RoleAssistant, "looking up"),
		makeToolMessage("user_memory", retrieveResult(snippet("Pref", "dark mode"))),
		makeToolMessage("project_memory", retrieveResult(snippet("Arch", "microservices"))),
		makeMessage(models.RoleAssistant, "found them"),
		makeMessage(models.RoleUser, "ok"),
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	userIdx := strings.Index(result, "user_memory.retrieve")
	projIdx := strings.Index(result, "project_memory.retrieve")
	if userIdx < 0 || projIdx < 0 {
		t.Fatal("expected both tool sections")
	}
	if userIdx > projIdx {
		t.Error("user_memory should appear before project_memory (canonical order)")
	}
}

func TestMostRecentWins(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "first retrieve"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", retrieveResult(snippet("Old", "old data"))),
		makeMessage(models.RoleAssistant, "old result"),
		makeMessage(models.RoleUser, "retrieve again"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", retrieveResult(snippet("New", "new data"))),
		makeMessage(models.RoleAssistant, "new result"),
		makeMessage(models.RoleUser, "thanks"),
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	if !strings.Contains(result, "New") {
		t.Error("expected most recent snippet")
	}
	if strings.Contains(result, "Old") {
		t.Error("should not contain older snippet for same tool")
	}
}

func TestNonRetrieveActionIgnored(t *testing.T) {
	summaryContent, _ := json.Marshal(map[string]any{
		"action":  "summary",
		"summary": "Some summary text",
	})
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "summarize memories"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", string(summaryContent)),
		makeMessage(models.RoleAssistant, "done"),
		makeMessage(models.RoleUser, "thanks"),
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	if result != "" {
		t.Errorf("expected empty overlay for non-retrieve action, got: %s", result)
	}
}

func TestMalformedJSON(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "get memories"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", "this is not json at all"),
		makeMessage(models.RoleAssistant, "hmm"),
		makeMessage(models.RoleUser, "ok"),
	}

	// Non-JSON won't pass isRetrieveAction, so it should be excluded.
	result := buildShortTermMemoryOverlay(history, testOptions)
	if result != "" {
		t.Errorf("expected empty overlay for non-JSON content, got: %s", result)
	}
}

func TestSizeLimits(t *testing.T) {
	largeText := strings.Repeat("x", 1000)
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "get"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", retrieveResult(snippet("Big", largeText))),
		makeMessage(models.RoleAssistant, "done"),
		makeMessage(models.RoleUser, "ok"),
	}

	opts := shortTermMemoryOverlayOptions{
		TurnTTL:         3,
		MaxItemsPerTool: 5,
		MaxCharsPerItem: 50,
		MaxCharsTotal:   4000,
	}

	result := buildShortTermMemoryOverlay(history, opts)
	if result == "" {
		t.Fatal("expected overlay")
	}
	// The snippet should be truncated to ~50 chars, not 1000.
	if strings.Contains(result, largeText) {
		t.Error("expected large snippet to be truncated")
	}
}

func TestTotalSizeLimit(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "get"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", retrieveResult(
			snippet("A", strings.Repeat("a", 300)),
			snippet("B", strings.Repeat("b", 300)),
			snippet("C", strings.Repeat("c", 300)),
		)),
		makeToolMessage("project_memory", retrieveResult(
			snippet("D", strings.Repeat("d", 300)),
		)),
		makeToolMessage("agent_memory", retrieveResult(
			snippet("E", strings.Repeat("e", 300)),
		)),
		makeMessage(models.RoleAssistant, "done"),
		makeMessage(models.RoleUser, "ok"),
	}

	opts := shortTermMemoryOverlayOptions{
		TurnTTL:         3,
		MaxItemsPerTool: 5,
		MaxCharsPerItem: 400,
		MaxCharsTotal:   500, // Very small total limit.
	}

	result := buildShortTermMemoryOverlay(history, opts)
	if len(result) > 600 { // Allow some overhead for tags/headers.
		t.Errorf("overlay too large: %d chars", len(result))
	}
}

func TestNoMemoryTools(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "hello"),
		makeMessage(models.RoleAssistant, "hi there"),
		makeMessage(models.RoleUser, "how are you"),
		makeMessage(models.RoleAssistant, "good"),
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	if result != "" {
		t.Errorf("expected empty overlay, got: %s", result)
	}
}

func TestCrossSummaryBoundary(t *testing.T) {
	// The overlay scans full history, including messages before a context_summary.
	summaryStopReason := models.StopReasonContextSummary
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "retrieve"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("user_memory", retrieveResult(snippet("Key", "value"))),
		makeMessage(models.RoleAssistant, "here's your data"),
		// Context summary boundary.
		{
			Role:       ptrRole(models.RoleAssistant),
			Content:    json.RawMessage(`"Summary of conversation so far."`),
			StopReason: &summaryStopReason,
		},
		makeMessage(models.RoleUser, "continue"),
	}

	result := buildShortTermMemoryOverlay(history, testOptions)
	if result == "" {
		t.Fatal("expected overlay to include results before summary boundary")
	}
	if !strings.Contains(result, "Key") {
		t.Error("expected snippet from before summary boundary")
	}
}

func TestParseRetrieveResult(t *testing.T) {
	content := retrieveResult(
		snippet("Title1", "body1", "tag1", "tag2"),
		snippet("Title2", "body2"),
	)

	snippets := parseRetrieveResult(content, 5, 400)
	if len(snippets) != 2 {
		t.Fatalf("expected 2 snippets, got %d", len(snippets))
	}
	if snippets[0].Title != "Title1" {
		t.Errorf("expected Title1, got %s", snippets[0].Title)
	}
	if len(snippets[0].Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(snippets[0].Tags))
	}
}

func TestSelectRecentSkipsNonMemoryTools(t *testing.T) {
	history := []*models.ConversationMessage{
		makeMessage(models.RoleUser, "do something"),
		makeMessage(models.RoleAssistant, "ok"),
		makeToolMessage("web_search", `{"action":"retrieve","snippets":[{"title":"X","snippet":"Y"}]}`),
		makeMessage(models.RoleAssistant, "done"),
		makeMessage(models.RoleUser, "ok"),
	}

	results := selectRecentMemoryRetrieveResults(history, 3)
	if len(results) != 0 {
		t.Errorf("expected no results for non-memory tool, got %d", len(results))
	}
}
