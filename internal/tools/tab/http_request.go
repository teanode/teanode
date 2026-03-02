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
	maxRequestBodySize  = 1 << 20 // 1 MB
	maxToolResultSize   = 768 << 10
	toolNameHTTPRequest = "tab.http_request"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&httpRequestTool{}}
	})
}

type httpRequestTool struct{}

func (t *httpRequestTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        toolNameHTTPRequest,
			Description: "Execute an HTTP request in the context of the attached browser tab. The request runs with the tab's cookies, session, and CORS policy. Supports relative URLs (resolved against the tab's current URL).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"method": map[string]interface{}{
						"type":    "string",
						"enum":    []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
						"default": "GET",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Absolute or relative URL.",
					},
					"headers": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
						"description":          "Request headers (key-value).",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Request body (for POST/PUT/PATCH).",
					},
					"timeout_ms": map[string]interface{}{
						"type":        "integer",
						"default":     30000,
						"description": "Request timeout in milliseconds.",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (t *httpRequestTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	origin := runners.OriginFromContext(ctx)
	if origin != "webui" {
		channel := origin
		if channel == "" {
			channel = "automated"
		}
		return jsonError(fmt.Sprintf("tab.http_request is only supported on the webui channel, not %s", channel)), nil
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

	var args struct {
		Method    string            `json:"method"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		Body      string            `json:"body"`
		TimeoutMs int               `json:"timeout_ms"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if args.URL == "" {
		return jsonError("url is required"), nil
	}
	if args.Method == "" {
		args.Method = "GET"
	}
	if len(args.Body) > maxRequestBodySize {
		return jsonError(fmt.Sprintf("request body too large (%d bytes, max %d)", len(args.Body), maxRequestBodySize)), nil
	}
	if args.TimeoutMs <= 0 {
		args.TimeoutMs = 30000
	}

	argsJSON, _ := json.Marshal(args)

	pending := &PendingToolCall{
		ID:             security.NewULID(),
		UserID:         user.ID,
		AgentID:        runner.AgentID,
		ConversationID: runner.ConversationID,
		ToolName:       toolNameHTTPRequest,
		Arguments:      argsJSON,
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
			"toolName":       toolNameHTTPRequest,
			"arguments":      args,
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

func jsonError(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}
