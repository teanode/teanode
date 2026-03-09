package askuser

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/integrations/questions"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
)

// stubRunner creates a minimal context with runner, user, broker, origin, and pubsub.
func stubContext(origin runners.Origin) (context.Context, *questions.QuestionBroker) {
	ctx := context.Background()
	broker := questions.NewQuestionBroker()
	ps := pubsub.New()

	// Create a minimal runner with exported fields.
	runner := &runners.Runner{
		ID:             "run1",
		AgentID:        "agent1",
		ConversationID: "conv1",
	}
	ctx = runners.ContextWithRunner(ctx, runner)
	ctx = runners.ContextWithOrigin(ctx, origin)
	ctx = questions.ContextWithQuestionBroker(ctx, broker)
	ctx = pubsub.ContextWithPubSub(ctx, ps)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "user1"}, nil, nil)
	return ctx, broker
}

func TestChannelGateTelegram(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx := runners.ContextWithOrigin(context.Background(), runners.OriginChannel)

	result, err := tool.Execute(ctx, `{"question":"Pick","choices":["A","B"]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if parsed["error"] == "" {
		t.Fatal("expected error in result for telegram channel")
	}
}

func TestChannelGateDiscord(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx := runners.ContextWithOrigin(context.Background(), runners.OriginChannel)

	result, err := tool.Execute(ctx, `{"question":"Pick","choices":["A","B"]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] == "" {
		t.Fatal("expected error in result for discord channel")
	}
}

func TestChannelGateAutomated(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx := runners.ContextWithOrigin(context.Background(), runners.OriginNone)

	result, err := tool.Execute(ctx, `{"question":"Pick","choices":["A","B"]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if parsed["error"] == "" {
		t.Fatal("expected error in result for automated channel")
	}
}

func TestHappyPath(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx, broker := stubContext(runners.OriginWeb)

	done := make(chan string, 1)
	go func() {
		result, err := tool.Execute(ctx, `{"question":"Pick a DB","choices":["PostgreSQL","SQLite"]}`)
		if err != nil {
			done <- "error:" + err.Error()
			return
		}
		done <- result
	}()

	// Give the goroutine time to register the question.
	time.Sleep(50 * time.Millisecond)

	pending := broker.PendingForConversation("conv1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending question, got %d", len(pending))
	}

	if err := broker.Answer(pending[0].ID, questions.AnswerPayload{Answer: "PostgreSQL"}); err != nil {
		t.Fatalf("failed to answer: %v", err)
	}

	select {
	case result := <-done:
		var parsed map[string]string
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}
		if parsed["answer"] != "PostgreSQL" {
			t.Errorf("expected PostgreSQL, got %s", parsed["answer"])
		}
		if _, hasOther := parsed["other"]; hasOther {
			t.Error("expected no 'other' key for normal choice")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Execute to return")
	}
}

func TestContextCancellation(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx, _ := stubContext(runners.OriginWeb)
	ctx, cancel := context.WithCancel(ctx)

	done := make(chan error, 1)
	go func() {
		_, err := tool.Execute(ctx, `{"question":"Pick","choices":["A","B"]}`)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Execute to return")
	}
}

func TestInvalidArguments(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx, _ := stubContext(runners.OriginWeb)

	// Missing question.
	_, err := tool.Execute(ctx, `{"choices":["A","B"]}`)
	if err == nil {
		t.Fatal("expected error for missing question")
	}

	// Too few choices.
	_, err = tool.Execute(ctx, `{"question":"Pick","choices":["A"]}`)
	if err == nil {
		t.Fatal("expected error for too few choices")
	}

	// Invalid JSON.
	_, err = tool.Execute(ctx, `not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNoBroker(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx := context.Background()
	ctx = runners.ContextWithOrigin(ctx, runners.OriginWeb)
	// No broker set.

	_, err := tool.Execute(ctx, `{"question":"Pick","choices":["A","B"]}`)
	if err == nil {
		t.Fatal("expected error when broker is not available")
	}
}

func TestHappyPathWithOther(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx, broker := stubContext(runners.OriginWeb)

	done := make(chan string, 1)
	go func() {
		result, err := tool.Execute(ctx, `{"question":"Pick a DB","choices":["PostgreSQL","SQLite"],"allowOther":true,"otherLabel":"Custom"}`)
		if err != nil {
			done <- "error:" + err.Error()
			return
		}
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)

	pending := broker.PendingForConversation("conv1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending question, got %d", len(pending))
	}
	if !pending[0].AllowOther {
		t.Error("expected AllowOther to be true")
	}
	if pending[0].OtherLabel != "Custom" {
		t.Errorf("expected OtherLabel 'Custom', got %s", pending[0].OtherLabel)
	}

	if err := broker.Answer(pending[0].ID, questions.AnswerPayload{Answer: "Custom", Other: "MongoDB"}); err != nil {
		t.Fatalf("failed to answer: %v", err)
	}

	select {
	case result := <-done:
		var parsed map[string]string
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}
		if parsed["answer"] != "Custom" {
			t.Errorf("expected 'Custom', got %s", parsed["answer"])
		}
		if parsed["other"] != "MongoDB" {
			t.Errorf("expected other 'MongoDB', got %s", parsed["other"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Execute to return")
	}
}

func TestAllowOtherDefaultValues(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx, broker := stubContext(runners.OriginWeb)

	done := make(chan string, 1)
	go func() {
		// Only allowOther, no otherLabel or otherPlaceholder.
		result, err := tool.Execute(ctx, `{"question":"Pick","choices":["A","B"],"allowOther":true}`)
		if err != nil {
			done <- "error:" + err.Error()
			return
		}
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)

	pending := broker.PendingForConversation("conv1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending question, got %d", len(pending))
	}
	if !pending[0].AllowOther {
		t.Error("expected AllowOther to be true")
	}
	if pending[0].OtherLabel != "" {
		t.Errorf("expected empty OtherLabel (uses frontend default), got %s", pending[0].OtherLabel)
	}

	broker.Answer(pending[0].ID, questions.AnswerPayload{Answer: "A"})
	<-done
}

func TestBackwardCompatibleNoAllowOther(t *testing.T) {
	tool := &askUserQuestionTool{}
	ctx, broker := stubContext(runners.OriginWeb)

	done := make(chan string, 1)
	go func() {
		// Old-style call without any allowOther fields.
		result, err := tool.Execute(ctx, `{"question":"Pick","choices":["X","Y"]}`)
		if err != nil {
			done <- "error:" + err.Error()
			return
		}
		done <- result
	}()

	time.Sleep(50 * time.Millisecond)

	pending := broker.PendingForConversation("conv1")
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending question, got %d", len(pending))
	}
	if pending[0].AllowOther {
		t.Error("expected AllowOther to be false by default")
	}

	broker.Answer(pending[0].ID, questions.AnswerPayload{Answer: "X"})

	select {
	case result := <-done:
		var parsed map[string]string
		json.Unmarshal([]byte(result), &parsed)
		if parsed["answer"] != "X" {
			t.Errorf("expected X, got %s", parsed["answer"])
		}
		if _, hasOther := parsed["other"]; hasOther {
			t.Error("expected no 'other' key")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
