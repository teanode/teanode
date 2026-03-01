package askuser

import (
	"testing"
	"time"
)

func TestRegisterAndAnswer(t *testing.T) {
	b := NewQuestionBroker()
	q := &PendingQuestion{
		ID:             "q1",
		ConversationID: "c1",
		UserID:         "u1",
		Question:       "Pick one",
		Choices:        []string{"A", "B"},
		answerChan:     MakeAnswerChan(),
	}
	b.Register(q)

	// Answer should deliver to the channel.
	go func() {
		if err := b.Answer("q1", AnswerPayload{Answer: "A"}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case payload := <-q.answerChan:
		if payload.Answer != "A" {
			t.Errorf("expected A, got %s", payload.Answer)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for answer")
	}
}

func TestAnswerUnknownID(t *testing.T) {
	b := NewQuestionBroker()
	err := b.Answer("nonexistent", AnswerPayload{Answer: "A"})
	if err == nil {
		t.Fatal("expected error for unknown question ID")
	}
}

func TestDoubleAnswer(t *testing.T) {
	b := NewQuestionBroker()
	q := &PendingQuestion{
		ID:         "q1",
		UserID:     "u1",
		answerChan: MakeAnswerChan(),
	}
	b.Register(q)

	if err := b.Answer("q1", AnswerPayload{Answer: "A"}); err != nil {
		t.Fatalf("first answer failed: %v", err)
	}

	// Second answer should fail.
	err := b.Answer("q1", AnswerPayload{Answer: "B"})
	if err == nil {
		t.Fatal("expected error on double answer")
	}
}

func TestCancel(t *testing.T) {
	b := NewQuestionBroker()
	q := &PendingQuestion{
		ID:         "q1",
		UserID:     "u1",
		answerChan: MakeAnswerChan(),
	}
	b.Register(q)
	b.Cancel("q1")

	// Channel should be closed.
	select {
	case _, ok := <-q.answerChan:
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}

	// After cancel, answer should fail.
	err := b.Answer("q1", AnswerPayload{Answer: "A"})
	if err == nil {
		t.Fatal("expected error after cancel")
	}
}

func TestCancelNonexistent(t *testing.T) {
	b := NewQuestionBroker()
	// Should not panic.
	b.Cancel("nonexistent")
}

func TestPendingForConversation(t *testing.T) {
	b := NewQuestionBroker()
	b.Register(&PendingQuestion{ID: "q1", ConversationID: "c1", UserID: "u1", answerChan: MakeAnswerChan()})
	b.Register(&PendingQuestion{ID: "q2", ConversationID: "c2", UserID: "u1", answerChan: MakeAnswerChan()})
	b.Register(&PendingQuestion{ID: "q3", ConversationID: "c1", UserID: "u2", answerChan: MakeAnswerChan()})

	result := b.PendingForConversation("c1")
	if len(result) != 2 {
		t.Fatalf("expected 2 questions for c1, got %d", len(result))
	}

	ids := map[string]bool{}
	for _, q := range result {
		ids[q.ID] = true
	}
	if !ids["q1"] || !ids["q3"] {
		t.Errorf("expected q1 and q3, got %v", ids)
	}

	result = b.PendingForConversation("c2")
	if len(result) != 1 || result[0].ID != "q2" {
		t.Errorf("expected [q2] for c2, got %v", result)
	}

	result = b.PendingForConversation("nonexistent")
	if len(result) != 0 {
		t.Errorf("expected empty for nonexistent, got %d", len(result))
	}
}

func TestVerifyOwnership(t *testing.T) {
	b := NewQuestionBroker()
	b.Register(&PendingQuestion{ID: "q1", UserID: "u1", answerChan: MakeAnswerChan()})

	// Correct user.
	if err := b.VerifyOwnership("q1", "u1"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Wrong user.
	if err := b.VerifyOwnership("q1", "u2"); err == nil {
		t.Fatal("expected error for wrong user")
	}

	// Unknown question.
	if err := b.VerifyOwnership("nonexistent", "u1"); err == nil {
		t.Fatal("expected error for unknown question")
	}
}

func TestMultipleConcurrentQuestions(t *testing.T) {
	b := NewQuestionBroker()
	questions := make([]*PendingQuestion, 3)
	for i := 0; i < 3; i++ {
		questions[i] = &PendingQuestion{
			ID:             "q" + string(rune('0'+i)),
			ConversationID: "c1",
			UserID:         "u1",
			answerChan:     MakeAnswerChan(),
		}
		b.Register(questions[i])
	}

	// Answer in reverse order.
	for i := 2; i >= 0; i-- {
		answer := "answer-" + string(rune('0'+i))
		if err := b.Answer(questions[i].ID, AnswerPayload{Answer: answer}); err != nil {
			t.Fatalf("failed to answer q%d: %v", i, err)
		}
	}

	// All channels should have received their answers.
	for i := 0; i < 3; i++ {
		expected := "answer-" + string(rune('0'+i))
		select {
		case payload := <-questions[i].answerChan:
			if payload.Answer != expected {
				t.Errorf("q%d: expected %s, got %s", i, expected, payload.Answer)
			}
		case <-time.After(time.Second):
			t.Fatalf("q%d: timed out", i)
		}
	}
}

func TestAnswerWithOtherPayload(t *testing.T) {
	b := NewQuestionBroker()
	q := &PendingQuestion{
		ID:         "q1",
		UserID:     "u1",
		AllowOther: true,
		OtherLabel: "Custom",
		answerChan: MakeAnswerChan(),
	}
	b.Register(q)

	go func() {
		if err := b.Answer("q1", AnswerPayload{Answer: "Custom", Other: "my freeform text"}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case payload := <-q.answerChan:
		if payload.Answer != "Custom" {
			t.Errorf("expected answer 'Custom', got %s", payload.Answer)
		}
		if payload.Other != "my freeform text" {
			t.Errorf("expected other 'my freeform text', got %s", payload.Other)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for answer")
	}
}

// ── AnswerBatch tests ─────────────────────────────────────────────────

func TestAnswerBatchHappyPath(t *testing.T) {
	b := NewQuestionBroker()
	q1 := &PendingQuestion{ID: "q1", UserID: "u1", answerChan: MakeAnswerChan()}
	q2 := &PendingQuestion{ID: "q2", UserID: "u1", answerChan: MakeAnswerChan()}
	b.Register(q1)
	b.Register(q2)

	answers := map[string]AnswerPayload{
		"q1": {Answer: "A"},
		"q2": {Answer: "B", Other: "custom"},
	}
	if err := b.AnswerBatch(answers, "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both channels should have received their answers.
	select {
	case p := <-q1.answerChan:
		if p.Answer != "A" {
			t.Errorf("q1: expected A, got %s", p.Answer)
		}
	case <-time.After(time.Second):
		t.Fatal("q1: timed out")
	}
	select {
	case p := <-q2.answerChan:
		if p.Answer != "B" || p.Other != "custom" {
			t.Errorf("q2: expected B/custom, got %s/%s", p.Answer, p.Other)
		}
	case <-time.After(time.Second):
		t.Fatal("q2: timed out")
	}

	// Both should be removed from pending.
	if pending := b.PendingForConversation(""); len(pending) != 0 {
		t.Errorf("expected no pending questions, got %d", len(pending))
	}
}

func TestAnswerBatchRejectsUnknownQuestion(t *testing.T) {
	b := NewQuestionBroker()
	q1 := &PendingQuestion{ID: "q1", ConversationID: "c1", UserID: "u1", answerChan: MakeAnswerChan()}
	b.Register(q1)

	answers := map[string]AnswerPayload{
		"q1":          {Answer: "A"},
		"nonexistent": {Answer: "B"},
	}
	err := b.AnswerBatch(answers, "u1")
	if err == nil {
		t.Fatal("expected error for unknown question")
	}

	// q1 should NOT have been answered (atomic rollback).
	select {
	case <-q1.answerChan:
		t.Fatal("q1 should not have received an answer")
	default:
		// Good — channel is empty.
	}

	// q1 should still be pending.
	pending := b.PendingForConversation("c1")
	if len(pending) != 1 || pending[0].ID != "q1" {
		t.Error("q1 should still be pending after failed batch")
	}
}

func TestAnswerBatchRejectsWrongUser(t *testing.T) {
	b := NewQuestionBroker()
	q1 := &PendingQuestion{ID: "q1", ConversationID: "c1", UserID: "u1", answerChan: MakeAnswerChan()}
	q2 := &PendingQuestion{ID: "q2", ConversationID: "c1", UserID: "u2", answerChan: MakeAnswerChan()}
	b.Register(q1)
	b.Register(q2)

	// u1 tries to answer q2 which belongs to u2.
	answers := map[string]AnswerPayload{
		"q1": {Answer: "A"},
		"q2": {Answer: "B"},
	}
	err := b.AnswerBatch(answers, "u1")
	if err == nil {
		t.Fatal("expected error for wrong user")
	}

	// Neither should have been answered.
	select {
	case <-q1.answerChan:
		t.Fatal("q1 should not have received an answer")
	default:
	}
	select {
	case <-q2.answerChan:
		t.Fatal("q2 should not have received an answer")
	default:
	}
}

func TestAnswerBatchEmptyMap(t *testing.T) {
	b := NewQuestionBroker()
	// Empty batch should succeed (no-op).
	if err := b.AnswerBatch(map[string]AnswerPayload{}, "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPendingQuestionAllowOtherFields(t *testing.T) {
	b := NewQuestionBroker()
	q := &PendingQuestion{
		ID:               "q1",
		ConversationID:   "c1",
		UserID:           "u1",
		AllowOther:       true,
		OtherLabel:       "Something else",
		OtherPlaceholder: "Describe...",
		answerChan:       MakeAnswerChan(),
	}
	b.Register(q)

	pending := b.PendingForConversation("c1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if !pending[0].AllowOther {
		t.Error("expected AllowOther to be true")
	}
	if pending[0].OtherLabel != "Something else" {
		t.Errorf("expected OtherLabel 'Something else', got %s", pending[0].OtherLabel)
	}
	if pending[0].OtherPlaceholder != "Describe..." {
		t.Errorf("expected OtherPlaceholder 'Describe...', got %s", pending[0].OtherPlaceholder)
	}
}
