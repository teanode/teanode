package voice

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

func TestContextPruning_FitsInBudget(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", "You are an assistant."),
	}
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

func TestContextPruning_ExceedsBudget(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", strings.Repeat("s", 20)),
	}
	for i := 0; i < 20; i++ {
		msgs = append(msgs,
			makeMsg("user", strings.Repeat("u", 1000)),
			makeMsg("assistant", strings.Repeat("a", 1000)),
		)
	}

	budget := 2000
	result := pruneVoiceContext(msgs, budget)

	if len(result) >= len(msgs) {
		t.Errorf("expected pruning to reduce %d messages, got %d", len(msgs), len(result))
	}

	if result[0].Role != "system" {
		t.Errorf("expected system prompt at index 0, got role=%q", result[0].Role)
	}

	orig := msgs[1:]
	last2 := orig[len(orig)-2:]
	got := result[len(result)-2:]
	for i, want := range last2 {
		if got[i].Role != want.Role {
			t.Errorf("last2[%d] role: want %q got %q", i, want.Role, got[i].Role)
		}
	}

	est := approxTokens(result)
	if est > budget+300 {
		t.Errorf("estimated tokens %d exceeds budget %d by more than one-message margin", est, budget)
	}
}

func TestContextPruning_AlwaysKeepsLast2(t *testing.T) {
	msgs := []providers.ChatMessage{
		makeMsg("system", "sys"),
		makeMsg("user", strings.Repeat("u", 100)),
		makeMsg("assistant", strings.Repeat("a", 100)),
		makeMsg("user", strings.Repeat("u", 100)),
		makeMsg("assistant", strings.Repeat("a", 100)),
	}

	budget := 60
	result := pruneVoiceContext(msgs, budget)

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
	if result[0].Role != "system" {
		t.Errorf("system prompt missing after pruning; first role=%q", result[0].Role)
	}
}
