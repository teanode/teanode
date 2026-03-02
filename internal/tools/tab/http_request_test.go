package tab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
)

// testContext builds a context enriched with user, runner, origin, pubsub, and tab broker.
func testContext(broker *TabToolBroker) context.Context {
	ctx := context.Background()
	user := &models.User{ID: "u1"}
	ctx = models.ContextWithUserSessionToken(ctx, user, nil, nil)
	ctx = runners.ContextWithOrigin(ctx, "webui")
	ctx = pubsub.ContextWithPubSub(ctx, pubsub.New())
	ctx = ContextWithTabToolBroker(ctx, broker)

	runner := &runners.Runner{
		ID:             "run1",
		AgentID:        "a1",
		ConversationID: "c1",
	}
	ctx = runners.ContextWithRunner(ctx, runner)
	return ctx
}

func TestHTTPRequestTool_NoAttachment(t *testing.T) {
	broker := NewTabToolBroker()
	ctx := testContext(broker)

	tool := &httpRequestTool{}
	result, err := tool.Execute(ctx, `{"url":"/api/test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"], "no browser tab attached") {
		t.Errorf("expected 'no browser tab attached' error, got: %s", result)
	}
}

func TestHTTPRequestTool_HappyPath(t *testing.T) {
	broker := NewTabToolBroker()
	broker.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://example.com",
	}, "conn1")

	ctx := testContext(broker)

	tool := &httpRequestTool{}

	// Resolve the pending call in a goroutine.
	go func() {
		// Wait for the pending call to be registered.
		time.Sleep(50 * time.Millisecond)
		broker.mu.Lock()
		var pendingID string
		for id := range broker.pending {
			pendingID = id
			break
		}
		broker.mu.Unlock()

		if pendingID != "" {
			broker.Resolve(pendingID, ToolCallResult{
				Result: `{"status":200,"body":"hello"}`,
			})
		}
	}()

	result, err := tool.Execute(ctx, `{"url":"/api/test","method":"GET"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":200`) {
		t.Errorf("expected status 200 in result, got: %s", result)
	}
}

func TestHTTPRequestTool_ContextCancel(t *testing.T) {
	broker := NewTabToolBroker()
	broker.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://example.com",
	}, "conn1")

	ctx := testContext(broker)
	ctx, cancel := context.WithCancel(ctx)

	tool := &httpRequestTool{}

	// Cancel context shortly after.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := tool.Execute(ctx, `{"url":"/api/test"}`)
	if err == nil {
		t.Fatal("expected error on context cancel")
	}
}

func TestHTTPRequestTool_OversizedBody(t *testing.T) {
	broker := NewTabToolBroker()
	broker.Attach(TabAttachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://example.com",
	}, "conn1")

	ctx := testContext(broker)

	tool := &httpRequestTool{}
	bigBody := strings.Repeat("x", maxRequestBodySize+1)
	result, err := tool.Execute(ctx, `{"url":"/api/test","body":"`+bigBody+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"], "request body too large") {
		t.Errorf("expected 'request body too large' error, got: %s", result)
	}
}

func TestHTTPRequestTool_NonWebuiOrigin(t *testing.T) {
	ctx := context.Background()
	user := &models.User{ID: "u1"}
	ctx = models.ContextWithUserSessionToken(ctx, user, nil, nil)
	ctx = runners.ContextWithOrigin(ctx, "telegram")
	broker := NewTabToolBroker()
	ctx = ContextWithTabToolBroker(ctx, broker)

	tool := &httpRequestTool{}
	result, err := tool.Execute(ctx, `{"url":"/api/test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	if !strings.Contains(parsed["error"], "only supported on the webui channel") {
		t.Errorf("expected webui-only error, got: %s", result)
	}
}
