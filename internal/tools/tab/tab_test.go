package tab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/integrations/tabs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
)

// testContext builds a context enriched with user, runner, origin, pubsub, and tab broker.
func testContext(broker *tabs.TabBroker) context.Context {
	ctx := context.Background()
	user := &models.User{ID: "u1"}
	ctx = models.ContextWithUserSessionToken(ctx, user, nil, nil)
	ctx = runners.ContextWithOrigin(ctx, "webui")
	ctx = pubsub.ContextWithPubSub(ctx, pubsub.New())
	ctx = tabs.ContextWithTabBroker(ctx, broker)

	runner := &runners.Runner{
		ID:             "run1",
		AgentID:        "a1",
		ConversationID: "c1",
	}
	ctx = runners.ContextWithRunner(ctx, runner)
	return ctx
}

// attachedBroker returns a broker with a tab already attached for user u1/a1/c1.
func attachedBroker() *tabs.TabBroker {
	broker := tabs.NewTabBroker()
	broker.Attach(tabs.Attachment{
		UserID: "u1", AgentID: "a1", ConversationID: "c1",
		TabURL: "https://example.com",
	}, "conn1")
	return broker
}

// resolvePending waits briefly then resolves the first pending call.
func resolvePending(broker *tabs.TabBroker, result tabs.ToolCallResult) {
	go func() {
		time.Sleep(50 * time.Millisecond)
		pendingId := broker.FirstPendingID()
		if pendingId != "" {
			broker.Resolve(pendingId, result)
		}
	}()
}

func parseError(result string) string {
	var parsed map[string]string
	json.Unmarshal([]byte(result), &parsed)
	return parsed["error"]
}

// ---- fetch action tests ----

func TestTabTool_FetchNoAttachment(t *testing.T) {
	broker := tabs.NewTabBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"fetch","url":"/api/test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "no browser tab attached") {
		t.Errorf("expected 'no browser tab attached' error, got: %s", result)
	}
}

func TestTabTool_FetchHappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"status":200,"body":"hello"}`})

	result, err := tool.Execute(ctx, `{"action":"fetch","url":"/api/test","method":"GET"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status":200`) {
		t.Errorf("expected status 200 in result, got: %s", result)
	}
}

func TestTabTool_FetchContextCancel(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	ctx, cancel := context.WithCancel(ctx)

	tool := &tabTool{}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := tool.Execute(ctx, `{"action":"fetch","url":"/api/test"}`)
	if err == nil {
		t.Fatal("expected error on context cancel")
	}
}

func TestTabTool_FetchOversizedBody(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	bigBody := strings.Repeat("x", maxRequestBodySize+1)
	result, err := tool.Execute(ctx, `{"action":"fetch","url":"/api/test","body":"`+bigBody+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "request body too large") {
		t.Errorf("expected 'request body too large' error, got: %s", result)
	}
}

func TestTabTool_NonWebuiOrigin(t *testing.T) {
	ctx := context.Background()
	user := &models.User{ID: "u1"}
	ctx = models.ContextWithUserSessionToken(ctx, user, nil, nil)
	ctx = runners.ContextWithOrigin(ctx, "telegram")
	broker := tabs.NewTabBroker()
	ctx = tabs.ContextWithTabBroker(ctx, broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"fetch","url":"/api/test"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "only supported on the webui channel") {
		t.Errorf("expected webui-only error, got: %s", result)
	}
}

func TestTabTool_FetchURLRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"fetch"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "url is required") {
		t.Errorf("expected 'url is required' error, got: %s", result)
	}
}

func TestTabTool_GetCookieNameRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"getCookie"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "name is required") {
		t.Errorf("expected 'name is required' error, got: %s", result)
	}
}

func TestTabTool_ListCookiesHappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"cookies":[]}`})

	result, err := tool.Execute(ctx, `{"action":"listCookies"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "cookies") {
		t.Errorf("expected cookies in result, got: %s", result)
	}
}

func TestTabTool_UnknownAction(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	_, err := tool.Execute(ctx, `{"action":"unknown"}`)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown tab action") {
		t.Errorf("expected 'unknown tab action' error, got: %v", err)
	}
}

// ---- setCookie action tests ----

func TestTabTool_SetCookie_URLRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"setCookie","name":"foo","value":"bar"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "url is required") {
		t.Errorf("expected 'url is required' error, got: %s", result)
	}
}

func TestTabTool_SetCookie_NameRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"setCookie","url":"https://example.com","value":"bar"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "name is required") {
		t.Errorf("expected 'name is required' error, got: %s", result)
	}
}

func TestTabTool_SetCookie_ValueRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"setCookie","url":"https://example.com","name":"foo"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "value is required") {
		t.Errorf("expected 'value is required' error, got: %s", result)
	}
}

func TestTabTool_SetCookie_InvalidSameSite(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"setCookie","url":"https://example.com","name":"foo","value":"bar","sameSite":"bad"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "invalid sameSite") {
		t.Errorf("expected 'invalid sameSite' error, got: %s", result)
	}
}

func TestTabTool_SetCookie_HappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"cookie":{"name":"foo","value":"bar"}}`})

	result, err := tool.Execute(ctx, `{"action":"setCookie","url":"https://example.com","name":"foo","value":"bar"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"cookie"`) {
		t.Errorf("expected cookie in result, got: %s", result)
	}
}

// ---- deleteCookie action tests ----

func TestTabTool_DeleteCookie_URLRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"deleteCookie","name":"foo"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "url is required") {
		t.Errorf("expected 'url is required' error, got: %s", result)
	}
}

func TestTabTool_DeleteCookie_NameRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"deleteCookie","url":"https://example.com"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "name is required") {
		t.Errorf("expected 'name is required' error, got: %s", result)
	}
}

func TestTabTool_DeleteCookie_HappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"ok":true}`})

	result, err := tool.Execute(ctx, `{"action":"deleteCookie","url":"https://example.com","name":"foo"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"ok":true`) {
		t.Errorf("expected ok:true in result, got: %s", result)
	}
}

// ---- localStorage action tests ----

func TestTabTool_SetLocalStorage_KeyRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"setLocalStorage","value":"bar"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "key is required") {
		t.Errorf("expected 'key is required' error, got: %s", result)
	}
}

func TestTabTool_SetLocalStorage_ValueTooLarge(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	bigVal := strings.Repeat("v", maxLocalStorageValue+1)
	result, err := tool.Execute(ctx, `{"action":"setLocalStorage","key":"k","value":"`+bigVal+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "value too large") {
		t.Errorf("expected 'value too large' error, got: %s", result)
	}
}

func TestTabTool_RemoveLocalStorage_KeyRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"removeLocalStorage"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "key is required") {
		t.Errorf("expected 'key is required' error, got: %s", result)
	}
}

func TestTabTool_GetLocalStorage_HappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"entries":{"foo":"bar"},"truncated":false}`})

	result, err := tool.Execute(ctx, `{"action":"getLocalStorage"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "foo") {
		t.Errorf("expected 'foo' in result, got: %s", result)
	}
}

func TestTabTool_GetLocalStorage_WithKey(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"value":"bar"}`})

	result, err := tool.Execute(ctx, `{"action":"getLocalStorage","key":"foo"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "bar") {
		t.Errorf("expected 'bar' in result, got: %s", result)
	}
}

func TestTabTool_SetLocalStorage_HappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"ok":true}`})

	result, err := tool.Execute(ctx, `{"action":"setLocalStorage","key":"foo","value":"bar"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"ok":true`) {
		t.Errorf("expected ok:true in result, got: %s", result)
	}
}

// ---- DOM action tests ----

func TestTabTool_QuerySelector_SelectorRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"querySelector"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "selector is required") {
		t.Errorf("expected 'selector is required' error, got: %s", result)
	}
}

func TestTabTool_QuerySelector_InvalidMode(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"querySelector","selector":"div","mode":"xml"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "invalid mode") {
		t.Errorf("expected 'invalid mode' error, got: %s", result)
	}
}

func TestTabTool_QuerySelector_DefaultMode(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"results":[{"tagName":"div","content":"hello","attributes":{}}]}`})

	result, err := tool.Execute(ctx, `{"action":"querySelector","selector":"div"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "results") {
		t.Errorf("expected 'results' in response, got: %s", result)
	}
}

func TestTabTool_QuerySelector_Truncated(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	// Build a results array that exceeds maxDomResultSize.
	elem := `{"tagName":"div","content":"` + strings.Repeat("a", 1024) + `","attributes":{}}`
	var elems []string
	for i := 0; i < (maxDomResultSize/1024)+10; i++ {
		elems = append(elems, elem)
	}
	bigResult := `{"results":[` + strings.Join(elems, ",") + `]}`
	resolvePending(broker, tabs.ToolCallResult{Result: bigResult})

	result, err := tool.Execute(ctx, `{"action":"querySelector","selector":"div","all":true}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) > maxDomResultSize {
		t.Errorf("result should be at most %d bytes, got %d", maxDomResultSize, len(result))
	}

	var parsed struct {
		Results   []json.RawMessage `json:"results"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result should be valid JSON: %v", err)
	}
	if !parsed.Truncated {
		t.Error("expected truncated to be true")
	}
	if len(parsed.Results) >= len(elems) {
		t.Errorf("expected fewer results after truncation, got %d (original %d)", len(parsed.Results), len(elems))
	}
}

func TestTabTool_SnapshotDom_HappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"html":"<html></html>","truncated":false}`})

	result, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "html") {
		t.Errorf("expected html in result, got: %s", result)
	}
}

func TestTabTool_SnapshotDom_Truncated(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	bigHTML := strings.Repeat("x", maxDomResultSize+100)
	resolvePending(broker, tabs.ToolCallResult{
		Result: `{"html":"` + bigHTML + `","truncated":false}`,
	})

	result, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var snap struct {
		HTML      string `json:"html"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &snap); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if !snap.Truncated {
		t.Error("expected truncated to be true")
	}
	if len(snap.HTML) != maxDomResultSize {
		t.Errorf("expected html length %d, got %d", maxDomResultSize, len(snap.HTML))
	}
}

func TestTabTool_SnapshotDom_NotTruncated(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{
		Result: `{"html":"<html>small</html>","truncated":false}`,
	})

	result, err := tool.Execute(ctx, `{"action":"snapshot"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var snap struct {
		HTML      string `json:"html"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &snap); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if snap.Truncated {
		t.Error("expected truncated to be false for small snapshot")
	}
	if snap.HTML != "<html>small</html>" {
		t.Errorf("unexpected html: %s", snap.HTML)
	}
}

func TestTabTool_Snapshot_TextMode(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"text":"Hello world","truncated":false}`})

	result, err := tool.Execute(ctx, `{"action":"snapshot","mode":"text"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Hello world") {
		t.Errorf("expected text content in result, got: %s", result)
	}
}

func TestTabTool_Snapshot_TextModeTruncated(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	bigText := strings.Repeat("x", maxDomResultSize+100)
	resolvePending(broker, tabs.ToolCallResult{
		Result: `{"text":"` + bigText + `","truncated":false}`,
	})

	result, err := tool.Execute(ctx, `{"action":"snapshot","mode":"text"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var snap struct {
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result), &snap); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}
	if !snap.Truncated {
		t.Error("expected truncated to be true")
	}
	if len(snap.Text) != maxDomResultSize {
		t.Errorf("expected text length %d, got %d", maxDomResultSize, len(snap.Text))
	}
}

func TestTabTool_Snapshot_InvalidMode(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"snapshot","mode":"xml"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "invalid mode") {
		t.Errorf("expected 'invalid mode' error, got: %s", result)
	}
}

// ---- eval action tests ----

func TestTabTool_Eval_CodeRequired(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	result, err := tool.Execute(ctx, `{"action":"eval"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "code is required") {
		t.Errorf("expected 'code is required' error, got: %s", result)
	}
}

func TestTabTool_Eval_CodeTooLarge(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)

	tool := &tabTool{}
	bigCode := strings.Repeat("x", maxEvalCodeSize+1)
	result, err := tool.Execute(ctx, `{"action":"eval","code":"`+bigCode+`"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(parseError(result), "code too large") {
		t.Errorf("expected 'code too large' error, got: %s", result)
	}
}

func TestTabTool_Eval_HappyPath(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"value":42,"truncated":false}`})

	result, err := tool.Execute(ctx, `{"action":"eval","code":"1+1"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "42") {
		t.Errorf("expected '42' in result, got: %s", result)
	}
}

func TestTabTool_Eval_ErrorResult(t *testing.T) {
	broker := attachedBroker()
	ctx := testContext(broker)
	tool := &tabTool{}

	resolvePending(broker, tabs.ToolCallResult{Result: `{"error":{"message":"ReferenceError: x is not defined","name":"ReferenceError"}}`})

	result, err := tool.Execute(ctx, `{"action":"eval","code":"x"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "ReferenceError") {
		t.Errorf("expected ReferenceError in result, got: %s", result)
	}
}

// ---- tool definition test ----

func TestTabTool_Definition_ContainsNewActions(t *testing.T) {
	tool := &tabTool{}
	def := tool.Definition()

	params := def.Function.Parameters.(map[string]interface{})
	props := params["properties"].(map[string]interface{})
	actionProp := props["action"].(map[string]interface{})
	actions := actionProp["enum"].([]string)

	expected := map[string]bool{
		"fetch": true, "listCookies": true, "getCookie": true, "setCookie": true, "deleteCookie": true,
		"getLocalStorage": true, "setLocalStorage": true, "removeLocalStorage": true,
		"snapshot": true, "querySelector": true, "eval": true,
	}

	for _, a := range actions {
		if !expected[a] {
			t.Errorf("unexpected action in definition: %s", a)
		}
		delete(expected, a)
	}

	for a := range expected {
		t.Errorf("missing action in definition: %s", a)
	}

	// Verify new parameters exist.
	for _, param := range []string{"key", "value", "selector", "mode", "all", "code", "path", "secure", "httpOnly", "sameSite", "expirationDate"} {
		if _, ok := props[param]; !ok {
			t.Errorf("missing parameter in definition: %s", param)
		}
	}
}
