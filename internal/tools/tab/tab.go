package tab

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/security"
)

const (
	maxRequestBodySize = 1 << 20   // 1 MB
	maxToolResultSize  = 768 << 10 // 768 KB
	maxEvalCodeSize    = 64 << 10  // 64 KB
	maxLocalStorageVal = 1 << 20   // 1 MB
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
				"listCookies / getCookie (cookie access), " +
				"getLocalStorage / setLocalStorage / removeLocalStorage (localStorage access), " +
				"snapshotDom (lightweight DOM snapshot), " +
				"querySelector (query DOM elements), " +
				"eval (execute JavaScript in page context).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{
							"fetch", "listCookies", "getCookie",
							"getLocalStorage", "setLocalStorage", "removeLocalStorage",
							"snapshotDom", "querySelector", "eval",
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
						"description": "Absolute or relative URL (required for fetch). URL scope (for listCookies, getCookie; defaults to tab URL if omitted).",
					},
					"headers": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
						"description":          "Request headers (for fetch action).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Request body for POST/PUT/PATCH (for fetch action).",
					},
					"timeoutMs": map[string]interface{}{
						"type":        "integer",
						"default":     30000,
						"description": "Request timeout in milliseconds (for fetch action).",
					},
					// cookie params
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Filter by domain (for listCookies action).",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Cookie name (required for getCookie, optional filter for listCookies).",
					},
					// localStorage params
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Storage key (optional for getLocalStorage — omit to get all; required for setLocalStorage and removeLocalStorage).",
					},
					"value": map[string]interface{}{
						"type":        "string",
						"description": "Value to store (required for setLocalStorage).",
					},
					// DOM params
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector (required for querySelector).",
					},
					"mode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"text", "html"},
						"default":     "text",
						"description": "Return mode for querySelector: 'text' returns textContent, 'html' returns outerHTML.",
					},
					"all": map[string]interface{}{
						"type":        "boolean",
						"default":     false,
						"description": "If true, querySelector returns all matching elements (querySelectorAll).",
					},
					// eval params
					"code": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript code to execute in the page context (for eval action). Must return a JSON-serializable value.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"description": "Action-dependent result. " +
					"fetch: {status, statusText, headers, body, url, truncated, durationMs}. " +
					"listCookies: {cookies}. getCookie: {cookie}. " +
					"getLocalStorage: {entries} or {value}. setLocalStorage/removeLocalStorage: {ok}. " +
					"snapshotDom: {html, truncated}. " +
					"querySelector: {results}. " +
					"eval: {value} or {error}. " +
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
						"description": "DOM HTML snapshot (snapshotDom).",
					},
					"results": map[string]interface{}{
						"type":        "array",
						"description": "Matched elements (querySelector).",
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
	Action    string            `json:"action"`
	Method    string            `json:"method,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	TimeoutMs int               `json:"timeoutMs,omitempty"`
	Domain    string            `json:"domain,omitempty"`
	Name      string            `json:"name,omitempty"`
	Key       string            `json:"key,omitempty"`
	Value     string            `json:"value,omitempty"`
	Selector  string            `json:"selector,omitempty"`
	Mode      string            `json:"mode,omitempty"`
	All       bool              `json:"all,omitempty"`
	Code      string            `json:"code,omitempty"`
}

func (self *tabTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	origin := runners.OriginFromContext(ctx)
	if origin != "webui" {
		channel := origin
		if channel == "" {
			channel = "automated"
		}
		return jsonError(fmt.Sprintf("tab tool is only supported on the webui channel, not %s", channel)), nil
	}

	broker := TabToolBrokerFromContext(ctx)
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

	if !broker.HasAttachment(user.ID, runner.AgentID, runner.ConversationID) {
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
		if arguments.TimeoutMs <= 0 {
			arguments.TimeoutMs = 30000
		}
	case "listCookies":
		// No required fields.
	case "getCookie":
		if arguments.Name == "" {
			return jsonError("name is required for getCookie action"), nil
		}

	// localStorage actions
	case "getLocalStorage":
		// key is optional: omit to get all entries.
	case "setLocalStorage":
		if arguments.Key == "" {
			return jsonError("key is required for setLocalStorage action"), nil
		}
		if len(arguments.Value) > maxLocalStorageVal {
			return jsonError(fmt.Sprintf("value too large (%d bytes, max %d)", len(arguments.Value), maxLocalStorageVal)), nil
		}
	case "removeLocalStorage":
		if arguments.Key == "" {
			return jsonError("key is required for removeLocalStorage action"), nil
		}

	// DOM actions
	case "snapshotDom":
		// No required fields.
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

	default:
		return "", fmt.Errorf("unknown tab action: %s", arguments.Action)
	}

	argumentsJSON, _ := json.Marshal(arguments)

	pending := &PendingToolCall{
		ID:             security.NewULID(),
		UserID:         user.ID,
		AgentID:        runner.AgentID,
		ConversationID: runner.ConversationID,
		ToolName:       "tab",
		Arguments:      argumentsJSON,
		resultChan:     MakeResultChan(),
	}
	broker.RegisterPending(pending)

	ps := pubsub.PubSubFromContext(ctx)
	if ps != nil {
		ps.Broadcast(pubsub.EventTypeTabToolCall, map[string]interface{}{
			"requestId":      pending.ID,
			"userId":         user.ID,
			"agentId":        runner.AgentID,
			"conversationId": runner.ConversationID,
			"toolName":       "tab",
			"arguments":      arguments,
		})
	}

	select {
	case result, ok := <-pending.resultChan:
		if !ok {
			return jsonError("tool call cancelled"), nil
		}
		if result.Error != "" {
			return jsonError(result.Error), nil
		}
		if len(result.Result) > maxToolResultSize {
			return result.Result[:maxToolResultSize], nil
		}
		return result.Result, nil
	case <-ctx.Done():
		broker.CancelPending(pending.ID)
		return "", ctx.Err()
	}
}

func jsonError(message string) string {
	data, _ := json.Marshal(map[string]string{"error": message})
	return string(data)
}
