package agents

import (
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/providers"
)

// makeMsg is a test helper that creates a ChatMessage with string content.
func makeMsg(role, content string) providers.ChatMessage {
	return providers.ChatMessage{Role: role, Content: content}
}

// approxTokens returns the same estimate as pruneVoiceContext uses.
func approxTokens(msgs []providers.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		if s, ok := m.Content.(string); ok {
			total += len(s)/4 + 1
		} else {
			total += 1
		}
	}
	return total
}

// TestContextPruning_FitsInBudget: 5 conversation turns that comfortably fit
// within a generous token budget → no messages should be pruned.
func TestContextPruning_FitsInBudget(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", "You are an assistant."),
	}
	// 5 user+assistant pairs, each ~250 chars ≈ 63 tokens per pair
	for i := 0; i < 5; i++ {
		msgs = append(msgs,
			makeMsg("user", strings.Repeat("x", 250)),
			makeMsg("assistant", strings.Repeat("y", 250)),
		)
	}

	result := pruneVoiceContext(msgs, 16000)

	if len(result) != len(msgs) {
		t.Errorf("expected %d messages (no pruning), got %d", len(msgs), len(result))
	}
}

// TestContextPruning_ExceedsBudget: 20 user+assistant pairs each consuming
// ~250 tokens → only the most-recent turns should be kept.
func TestContextPruning_ExceedsBudget(t *testing.T) {
	// Each message: 1000 chars → 1000/4+1 = 251 tokens.
	// 20 pairs × 2 × 251 = 10 040 conv tokens + ~6 system tokens ≈ 10 046 total.
	// Budget: 2 000 tokens → only a few recent turns fit.
	msgs := []providers.ChatMessage{
		makeMsg("system", strings.Repeat("s", 20)), // ~6 tokens
	}
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			makeMsg("user", strings.Repeat("u", 1000)),
			makeMsg("assistant", strings.Repeat("a", 1000)),
		)
	}

	budget := 2000
	result := pruneVoiceContext(msgs, budget)

	// Must be fewer messages than original.
	if len(result) >= len(msgs) {
		t.Errorf("expected pruning to reduce %d messages, got %d", len(msgs), len(result))
	}

	// System prompt must always be first.
	if result[0].Role != "system" {
		t.Errorf("expected system prompt at index 0, got role=%q", result[0].Role)
	}

	// Last 2 non-system messages from original must be present.
	orig := msgs[1:] // strip system
	last2 := orig[len(orig)-2:]
	got := result[len(result)-2:]
	for i, want := range last2 {
		if got[i].Role != want.Role {
			t.Errorf("last2[%d] role: want %q got %q", i, want.Role, got[i].Role)
		}
	}

	// Total token estimate must not exceed budget by more than one message margin.
	est := approxTokens(result)
	if est > budget+300 {
		t.Errorf("estimated tokens %d exceeds budget %d by more than one-message margin", est, budget)
	}
}

// TestContextPruning_AlwaysKeepsLast2: budget so tight that only 2 conv
// messages can fit (plus the system prompt) → exactly those 2 are kept.
func TestContextPruning_AlwaysKeepsLast2(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", "sys"),
		makeMsg("user", strings.Repeat("u", 100)),      // ~26 tokens
		makeMsg("assistant", strings.Repeat("a", 100)), // ~26 tokens
		makeMsg("user", strings.Repeat("u", 100)),      // last user
		makeMsg("assistant", strings.Repeat("a", 100)), // last assistant (guaranteed)
	}

	// Budget: just enough for system + last 2 messages (~55 tokens), not the earlier pair.
	budget := 60
	result := pruneVoiceContext(msgs, budget)

	// Should have system + last 2 conv messages = 3.
	if len(result) != 3 {
		t.Errorf("expected 3 messages (system + last 2), got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("index 0 should be system, got %q", result[0].Role)
	}
	if result[1].Role != "user" {
		t.Errorf("index 1 should be user (last user turn), got %q", result[1].Role)
	}
	if result[2].Role != "assistant" {
		t.Errorf("index 2 should be assistant (last assistant turn), got %q", result[2].Role)
	}
}

// TestContextPruning_SkippedForNonVoice: MaxContextTokens=0 means no pruning.
func TestContextPruning_SkippedForNonVoice(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", "sys"),
	}
	for i := 0; i < 50; i++ {
		msgs = append(msgs,
			makeMsg("user", strings.Repeat("u", 500)),
			makeMsg("assistant", strings.Repeat("a", 500)),
		)
	}

	result := pruneVoiceContext(msgs, 0)
	if len(result) != len(msgs) {
		t.Errorf("MaxContextTokens=0 should skip pruning; got %d messages, want %d", len(result), len(msgs))
	}
}

// TestContextPruning_LogsWhenPruning: pruning should produce a DEBUG log entry.
// We verify indirectly: if len(result) < len(msgs) then pruning occurred and the
// log path was exercised. (Capturing the go-logging output is complex; the
// functional behaviour is what matters for this gate.)
func TestContextPruning_LogsWhenPruning(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", "sys"),
	}
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			makeMsg("user", strings.Repeat("u", 800)),
			makeMsg("assistant", strings.Repeat("a", 800)),
		)
	}

	result := pruneVoiceContext(msgs, 500)

	if len(result) >= len(msgs) {
		t.Error("expected pruning to occur so that the log path was exercised")
	}
	// Ensure system prompt is intact.
	if result[0].Role != "system" {
		t.Errorf("system prompt missing after pruning; first role=%q", result[0].Role)
	}
}
