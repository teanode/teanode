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
		if err := b.Answer("q1", "A"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}()

	select {
	case answer := <-q.answerChan:
		if answer != "A" {
			t.Errorf("expected A, got %s", answer)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for answer")
	}
}

func TestAnswerUnknownID(t *testing.T) {
	b := NewQuestionBroker()
	err := b.Answer("nonexistent", "A")
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

	if err := b.Answer("q1", "A"); err != nil {
		t.Fatalf("first answer failed: %v", err)
	}

	// Second answer should fail.
	err := b.Answer("q1", "B")
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
	err := b.Answer("q1", "A")
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
		if err := b.Answer(questions[i].ID, answer); err != nil {
			t.Fatalf("failed to answer q%d: %v", i, err)
		}
	}

	// All channels should have received their answers.
	for i := 0; i < 3; i++ {
		expected := "answer-" + string(rune('0'+i))
		select {
		case answer := <-questions[i].answerChan:
			if answer != expected {
				t.Errorf("q%d: expected %s, got %s", i, expected, answer)
			}
		case <-time.After(time.Second):
			t.Fatalf("q%d: timed out", i)
		}
	}
}
