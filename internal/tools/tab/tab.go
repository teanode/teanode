// Package tab exposes tools for interacting with browser tabs.
package tab

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/integrations/tabs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/security"
)

const (
	maxRequestBodySize   = 1 << 20   // 1 MB
	maxToolResultSize    = 256 << 10 // 256 KB general fallback
	maxEvalCodeSize      = 64 << 10  // 64 KB
	maxLocalStorageValue = 1 << 20   // 1 MB
	maxFetchResultSize   = 128 << 10 // 128 KB for fetch response body
	maxDomResultSize     = 128 << 10 // 128 KB for snapshot / querySelector results
	maxSteps             = 50        // max steps in executeSteps
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&tabTool{}}
	})
}

type tabTool struct{}

func (self *tabTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "tab",
			Description: "Interact with the attached browser tab. Actions: " +
				"fetch (HTTP request with tab session), " +
				"listCookies / getCookie / setCookie / deleteCookie (cookie access), " +
				"getLocalStorage / setLocalStorage / removeLocalStorage (localStorage access), " +
				"snapshot (DOM snapshot; modes: 'html', 'text', 'accessibility', 'interactive' — " +
				"'interactive' returns an AI-friendly accessibility tree with [ref=N] markers on interactive elements for use with ref-based actions), " +
				"querySelector (query DOM elements), " +
				"eval (execute JavaScript in page context), " +
				"clickRef / typeRef / hoverRef / selectOption (interact with elements by ref from an interactive snapshot), " +
				"wait (wait for a page condition: selector, navigation, network_idle, or timeout), " +
				"executeSteps (run multiple tab actions atomically in sequence, keeping refs valid across steps).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{
							"fetch", "listCookies", "getCookie", "setCookie", "deleteCookie",
							"getLocalStorage", "setLocalStorage", "removeLocalStorage",
							"snapshot", "querySelector", "eval",
							"clickRef", "typeRef", "hoverRef", "selectOption",
							"wait", "executeSteps",
						},
						"description": "The tab action to perform.",
					},
					// fetch params
					"method": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
						"default":     "GET",
						"description": "HTTP method (for fetch action).",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative URL (required for fetch, setCookie, deleteCookie). URL scope (for listCookies, getCookie; defaults to tab URL if omitted).",
					},
					"headers": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
						"description":          "Additional HTTP request headers as key-value pairs (for fetch action). Use to set Authorization, Content-Type, Accept, or custom headers (e.g. X-Custom-Header). Example: {\"Authorization\": \"Bearer token\", \"Accept\": \"application/json\"}.",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Request body for POST/PUT/PATCH (for fetch action).",
					},
					"timeoutMs": map[string]interface{}{
						"type":        "integer",
						"default":     30000,
						"description": "Timeout in milliseconds. For fetch: request timeout (default 30000). For wait: condition timeout (default 30000). For executeSteps: total timeout (default 120000).",
					},
					// cookie params
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Cookie domain (filter for listCookies; optional for setCookie).",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Cookie path (for setCookie).",
					},
					"secure": map[string]interface{}{
						"type":        "boolean",
						"description": "Secure flag (for setCookie).",
					},
					"httpOnly": map[string]interface{}{
						"type":        "boolean",
						"description": "HttpOnly flag (for setCookie).",
					},
					"sameSite": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"no_restriction", "lax", "strict"},
						"description": "SameSite attribute (for setCookie).",
					},
					"expirationDate": map[string]interface{}{
						"type":        "number",
						"description": "Cookie expiration as Unix epoch seconds (for setCookie). Omit for session cookie.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Cookie name (required for getCookie, setCookie, deleteCookie; optional filter for listCookies).",
					},
					// localStorage params
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Storage key (optional for getLocalStorage — omit to get all; required for setLocalStorage and removeLocalStorage).",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "Value to store (required for setLocalStorage, setCookie).",
					},
					// DOM params
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector (required for querySelector; used by wait mode 'selector').",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"text", "html", "accessibility", "interactive"},
						"default":     "text",
						"description": "For querySelector: 'text' returns textContent, 'html' returns outerHTML. For snapshot: 'html' (default) returns cleaned HTML, 'text' returns visible text only, 'accessibility' returns the accessibility tree, 'interactive' returns an AI-friendly tree with [ref=N] markers on interactive elements.",
					},
					"all": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "If true, querySelector returns all matching elements (querySelectorAll).",
					},
					// eval params
					"code": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript code to execute in the page context (for eval action). Must evaluate to a JSON-serializable value.",
					},
					// ref-based action params
					"ref": map[string]interface{}{
						"type":        "integer",
						"description": "Element ref number from an interactive snapshot (required for clickRef, typeRef, hoverRef, selectOption).",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to type into the element (required for typeRef).",
					},
					"clearFirst": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "If true, clear the input field before typing (for typeRef).",
					},
					"optionValue": map[string]interface{}{
						"type":        "string",
						"description": "Value attribute of the option to select (for selectOption; provide optionValue or optionIndex).",
					},
					"optionIndex": map[string]interface{}{
						"type":        "integer",
						"description": "Zero-based index of the option to select (for selectOption; provide optionValue or optionIndex).",
					},
					// wait params
					"waitMode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"selector", "navigation", "network_idle", "timeout"},
						"description": "Wait condition type (for wait action). 'selector' waits for a CSS selector to match, 'navigation' waits for page load, 'network_idle' waits for network quiescence, 'timeout' waits for a fixed duration.",
					},
					// executeSteps params
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "Array of step objects for executeSteps. Each step has an 'action' field plus action-specific fields. Steps execute sequentially; execution stops on first error. Max 50 steps. Supports: snapshot, querySelector, eval, clickRef, typeRef, hoverRef, selectOption, wait, getLocalStorage, setLocalStorage, removeLocalStorage.",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"action": map[string]interface{}{
									"type":        "string",
									"description": "The action for this step.",
								},
							},
							"required": []string{"action"},
						},
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"description": "Action-dependent result. " +
					"fetch: {status, statusText, headers, body, url, truncated, durationMs}. " +
					"listCookies: {cookies}. getCookie: {cookie}. setCookie: {cookie}. deleteCookie: {ok}. " +
					"getLocalStorage: {entries} or {value}. setLocalStorage/removeLocalStorage: {ok}. " +
					"snapshot (html/text/accessibility): {html|text, truncated}. " +
					"snapshot (interactive): {tree, refCount, pageUrl, title, truncated}. " +
					"querySelector: {results}. " +
					"eval: {value} or {error}. " +
					"clickRef: {ref, role, name, clicked}. typeRef: {ref, role, text, clearFirst}. " +
					"hoverRef: {ref, role, name, x, y}. selectOption: {ref, selectedValue, selectedIndex, selectedText}. " +
					"wait: {mode, elapsed, ...mode-specific fields}. " +
					"executeSteps: {stepsExecuted, totalSteps, results: [{step, action, result?, error?}]}. " +
					"On error: {error}.",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type":        "integer",
						"description": "HTTP status code (fetch).",
					},
					"statusText": map[string]interface{}{
						"type":        "string",
						"description": "HTTP status text (fetch).",
					},
					"headers": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
						"description":          "Response headers (fetch).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Response body text (fetch).",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Final URL after redirects (fetch).",
					},
					"truncated": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the result was truncated.",
					},
					"durationMs": map[string]interface{}{
						"type":        "integer",
						"description": "Request duration in milliseconds (fetch).",
					},
					"cookies": map[string]interface{}{
						"type":        "array",
						"description": "List of cookies (listCookies).",
					},
					"cookie": map[string]interface{}{
						"type":        "object",
						"description": "Single cookie or null (getCookie).",
					},
					"entries": map[string]interface{}{
						"type":        "object",
						"description": "Key/value map of localStorage entries (getLocalStorage without key).",
					},
					"value": map[string]interface{}{
						"description": "Single value (getLocalStorage with key, eval result).",
					},
					"ok": map[string]interface{}{
						"type":        "boolean",
						"description": "Success flag (setLocalStorage, removeLocalStorage).",
					},
					"html": map[string]interface{}{
						"type":        "string",
						"description": "DOM HTML snapshot (snapshot).",
					},
					"tree": map[string]interface{}{
						"type":        "string",
						"description": "Interactive accessibility tree with [ref=N] markers (snapshot mode interactive).",
					},
					"refCount": map[string]interface{}{
						"type":        "integer",
						"description": "Number of interactive element refs assigned (snapshot mode interactive).",
					},
					"results": map[string]interface{}{
						"type":        "array",
						"description": "Matched elements (querySelector) or step results (executeSteps).",
					},
					"error": map[string]interface{}{
						"type":        "string",
						"description": "Error message if the action failed.",
					},
				},
			},
		},
	}
}

type tabArguments struct {
	Action              string                   `json:"action"`
	Method              string                   `json:"method,omitempty"`
	URL                 string                   `json:"url,omitempty"`
	Headers             map[string]string        `json:"headers,omitempty"`
	Body                string                   `json:"body,omitempty"`
	TimeoutMilliseconds int                      `json:"timeoutMs,omitempty"`
	Domain              string                   `json:"domain,omitempty"`
	Name                string                   `json:"name,omitempty"`
	Path                string                   `json:"path,omitempty"`
	Secure              *bool                    `json:"secure,omitempty"`
	HttpOnly            *bool                    `json:"httpOnly,omitempty"`
	SameSite            string                   `json:"sameSite,omitempty"`
	ExpirationDate      float64                  `json:"expirationDate,omitempty"`
	Key                 string                   `json:"key,omitempty"`
	Value               string                   `json:"value,omitempty"`
	Selector            string                   `json:"selector,omitempty"`
	Mode                string                   `json:"mode,omitempty"`
	All                 bool                     `json:"all,omitempty"`
	Code                string                   `json:"code,omitempty"`
	Ref                 *int                     `json:"ref,omitempty"`
	Text                string                   `json:"text,omitempty"`
	ClearFirst          bool                     `json:"clearFirst,omitempty"`
	OptionValue         string                   `json:"optionValue,omitempty"`
	OptionIndex         *int                     `json:"optionIndex,omitempty"`
	WaitMode            string                   `json:"waitMode,omitempty"`
	Steps               []map[string]interface{} `json:"steps,omitempty"`
}

func (self *tabTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"fetch", "listCookies", "getCookie", "getLocalStorage", "snapshot", "querySelector", "wait"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *tabTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	origin := runners.OriginFromContext(ctx)
	if origin != runners.OriginWeb {
		channel := string(origin)
		if channel == "" {
			channel = "automated"
		}
		return jsonError(fmt.Sprintf("tab tool is only supported on the webui channel, not %s", channel)), nil
	}

	broker := tabs.TabBrokerFromContext(ctx)
	if broker == nil {
		return jsonError("tab tool broker not available"), nil
	}

	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", fmt.Errorf("runner context not available")
	}
	user := models.UserFromContext(ctx)
	if user == nil {
		return "", fmt.Errorf("authentication required")
	}

	attachment := broker.GetAttachment(user.ID, runner.AgentID, runner.ConversationID)
	if attachment == nil {
		return jsonError("no browser tab attached to this conversation"), nil
	}

	var arguments tabArguments
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "fetch":
		if arguments.URL == "" {
			return jsonError("url is required for fetch action"), nil
		}
		if arguments.Method == "" {
			arguments.Method = "GET"
		}
		if len(arguments.Body) > maxRequestBodySize {
			return jsonError(fmt.Sprintf("request body too large (%d bytes, max %d)", len(arguments.Body), maxRequestBodySize)), nil
		}
		if arguments.TimeoutMilliseconds <= 0 {
			arguments.TimeoutMilliseconds = 30000
		}
	case "listCookies":
		// No required fields.
	case "getCookie":
		if arguments.Name == "" {
			return jsonError("name is required for getCookie action"), nil
		}
	case "setCookie":
		if arguments.URL == "" {
			return jsonError("url is required for setCookie action"), nil
		}
		if arguments.Name == "" {
			return jsonError("name is required for setCookie action"), nil
		}
		if arguments.Value == "" {
			return jsonError("value is required for setCookie action"), nil
		}
		if arguments.SameSite != "" {
			switch arguments.SameSite {
			case "no_restriction", "lax", "strict":
			default:
				return jsonError(fmt.Sprintf("invalid sameSite %q: must be 'no_restriction', 'lax', or 'strict'", arguments.SameSite)), nil
			}
		}
	case "deleteCookie":
		if arguments.URL == "" {
			return jsonError("url is required for deleteCookie action"), nil
		}
		if arguments.Name == "" {
			return jsonError("name is required for deleteCookie action"), nil
		}

	// localStorage actions
	case "getLocalStorage":
		// key is optional: omit to get all entries.
	case "setLocalStorage":
		if arguments.Key == "" {
			return jsonError("key is required for setLocalStorage action"), nil
		}
		if len(arguments.Value) > maxLocalStorageValue {
			return jsonError(fmt.Sprintf("value too large (%d bytes, max %d)", len(arguments.Value), maxLocalStorageValue)), nil
		}
	case "removeLocalStorage":
		if arguments.Key == "" {
			return jsonError("key is required for removeLocalStorage action"), nil
		}

	// DOM actions
	case "snapshot":
		if arguments.Mode == "" {
			arguments.Mode = "html"
		}
		if arguments.Mode != "html" && arguments.Mode != "text" && arguments.Mode != "accessibility" && arguments.Mode != "interactive" {
			return jsonError(fmt.Sprintf("invalid mode %q: must be 'html', 'text', 'accessibility', or 'interactive'", arguments.Mode)), nil
		}
	case "querySelector":
		if arguments.Selector == "" {
			return jsonError("selector is required for querySelector action"), nil
		}
		if arguments.Mode == "" {
			arguments.Mode = "text"
		}
		if arguments.Mode != "text" && arguments.Mode != "html" {
			return jsonError(fmt.Sprintf("invalid mode %q: must be 'text' or 'html'", arguments.Mode)), nil
		}

	// JS eval action
	case "eval":
		if arguments.Code == "" {
			return jsonError("code is required for eval action"), nil
		}
		if len(arguments.Code) > maxEvalCodeSize {
			return jsonError(fmt.Sprintf("code too large (%d bytes, max %d)", len(arguments.Code), maxEvalCodeSize)), nil
		}

	// ref-based actions
	case "clickRef":
		if arguments.Ref == nil {
			return jsonError("ref is required for clickRef action"), nil
		}
	case "typeRef":
		if arguments.Ref == nil {
			return jsonError("ref is required for typeRef action"), nil
		}
		if arguments.Text == "" {
			return jsonError("text is required for typeRef action"), nil
		}
	case "hoverRef":
		if arguments.Ref == nil {
			return jsonError("ref is required for hoverRef action"), nil
		}
	case "selectOption":
		if arguments.Ref == nil {
			return jsonError("ref is required for selectOption action"), nil
		}
		if arguments.OptionValue == "" && arguments.OptionIndex == nil {
			return jsonError("either optionValue or optionIndex is required for selectOption action"), nil
		}

	// wait action
	case "wait":
		if arguments.WaitMode == "" {
			return jsonError("waitMode is required for wait action"), nil
		}
		switch arguments.WaitMode {
		case "selector":
			if arguments.Selector == "" {
				return jsonError("selector is required for wait mode 'selector'"), nil
			}
		case "navigation", "network_idle", "timeout":
		default:
			return jsonError(fmt.Sprintf("invalid waitMode %q: must be 'selector', 'navigation', 'network_idle', or 'timeout'", arguments.WaitMode)), nil
		}
		if arguments.TimeoutMilliseconds <= 0 {
			arguments.TimeoutMilliseconds = 30000
		}

	// multi-step execution
	case "executeSteps":
		if len(arguments.Steps) == 0 {
			return jsonError("steps is required and must not be empty for executeSteps action"), nil
		}
		if len(arguments.Steps) > maxSteps {
			return jsonError(fmt.Sprintf("too many steps (%d, max %d)", len(arguments.Steps), maxSteps)), nil
		}
		if arguments.TimeoutMilliseconds <= 0 {
			arguments.TimeoutMilliseconds = 120000
		}

	default:
		return "", fmt.Errorf("unknown tab action: %s", arguments.Action)
	}

	argumentsJson, _ := json.Marshal(arguments)

	pending := &tabs.PendingToolCall{
		ID:             security.NewULID(),
		UserID:         user.ID,
		AgentID:        runner.AgentID,
		ConversationID: runner.ConversationID,
		ToolName:       "tab",
		Arguments:      argumentsJson,
	}
	pending.SetResultChan(tabs.MakeResultChan())
	broker.RegisterPending(pending)

	events := pubsub.PubSubFromContext(ctx)
	if events != nil {
		events.Broadcast(pubsub.EventTypeTabCommand, map[string]interface{}{
			"requestId":      pending.ID,
			"userId":         user.ID,
			"agentId":        runner.AgentID,
			"conversationId": runner.ConversationID,
			"toolName":       "tab",
			"arguments":      arguments,
			"tabId":          attachment.TabID,
		})
	}

	select {
	case result, ok := <-pending.ResultChan():
		if !ok {
			return jsonError("tool call cancelled"), nil
		}
		if result.Error != "" {
			return jsonError(result.Error), nil
		}
		out := result.Result
		switch arguments.Action {
		case "fetch":
			out = truncateFetchResult(out, maxFetchResultSize)
		case "snapshot":
			if arguments.Mode == "interactive" {
				out = truncateInteractiveSnapshot(out, maxDomResultSize)
			} else {
				out = truncateSnapshot(out, maxDomResultSize)
			}
		case "querySelector":
			out = truncateQuerySelector(out, maxDomResultSize)
		}
		if len(out) > maxToolResultSize {
			out = out[:maxToolResultSize]
		}
		return out, nil
	case <-ctx.Done():
		broker.CancelPending(pending.ID)
		return "", ctx.Err()
	}
}

// truncateFetchResult caps the body field to maxSize bytes, preserving valid JSON.
func truncateFetchResult(raw string, maxSize int) string {
	var resp struct {
		Status     int               `json:"status"`
		StatusText string            `json:"statusText,omitempty"`
		Headers    map[string]string `json:"headers,omitempty"`
		Body       string            `json:"body"`
		URL        string            `json:"url,omitempty"`
		Truncated  bool              `json:"truncated"`
		DurationMs int               `json:"durationMs,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &resp) != nil {
		if len(raw) > maxSize {
			return raw[:maxSize]
		}
		return raw
	}
	if len(resp.Body) <= maxSize {
		return raw
	}
	resp.Body = resp.Body[:maxSize]
	resp.Truncated = true
	out, _ := json.Marshal(resp)
	return string(out)
}

// truncateSnapshot caps the main content field (html or text) to maxSize bytes.
func truncateSnapshot(raw string, maxSize int) string {
	var snap struct {
		HTML      string `json:"html,omitempty"`
		Text      string `json:"text,omitempty"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal([]byte(raw), &snap) != nil {
		return raw
	}
	// Determine which field carries the content.
	content := snap.HTML
	isText := false
	if content == "" {
		content = snap.Text
		isText = true
	}
	if len(content) <= maxSize {
		return raw
	}
	content = content[:maxSize]
	if isText {
		snap.Text = content
	} else {
		snap.HTML = content
	}
	snap.Truncated = true
	out, _ := json.Marshal(snap)
	return string(out)
}

// truncateQuerySelector caps the raw querySelector result to maxSize bytes.
// It drops trailing elements from the results array until the output fits.
func truncateQuerySelector(raw string, maxSize int) string {
	if len(raw) <= maxSize {
		return raw
	}
	var qs struct {
		Results   []json.RawMessage `json:"results"`
		Truncated bool              `json:"truncated"`
	}
	if json.Unmarshal([]byte(raw), &qs) != nil || len(qs.Results) == 0 {
		// Can't parse or no results to trim — fall back to raw slice.
		return raw[:maxSize]
	}
	qs.Truncated = true
	for len(qs.Results) > 0 {
		out, _ := json.Marshal(qs)
		if len(out) <= maxSize {
			return string(out)
		}
		qs.Results = qs.Results[:len(qs.Results)-1]
	}
	out, _ := json.Marshal(qs)
	return string(out)
}

// truncateInteractiveSnapshot caps the tree field to maxSize bytes.
func truncateInteractiveSnapshot(raw string, maxSize int) string {
	var snap struct {
		Tree      string `json:"tree"`
		RefCount  int    `json:"refCount"`
		PageURL   string `json:"pageUrl,omitempty"`
		Title     string `json:"title,omitempty"`
		Truncated bool   `json:"truncated"`
	}
	if json.Unmarshal([]byte(raw), &snap) != nil {
		return raw
	}
	if len(snap.Tree) <= maxSize {
		return raw
	}
	snap.Tree = snap.Tree[:maxSize]
	snap.Truncated = true
	out, _ := json.Marshal(snap)
	return string(out)
}

func jsonError(message string) string {
	data, _ := json.Marshal(map[string]string{"error": message})
	return string(data)
}
