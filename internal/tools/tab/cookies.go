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
	toolNameCookiesList = "tab.cookies.list"
	toolNameCookiesGet  = "tab.cookies.get"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&cookiesListTool{}, &cookiesGetTool{}}
	})
}

// --- tab.cookies.list ---

type cookiesListTool struct{}

func (t *cookiesListTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        toolNameCookiesList,
			Description: "List cookies accessible to the attached browser tab, optionally filtered by URL, domain, or name.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Filter by URL. If omitted, uses attached tab URL.",
					},
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Filter by domain.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Filter by cookie name.",
					},
				},
			},
		},
	}
}

func (t *cookiesListTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	return executeTabTool(ctx, toolNameCookiesList, rawArguments)
}

// --- tab.cookies.get ---

type cookiesGetTool struct{}

func (t *cookiesGetTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        toolNameCookiesGet,
			Description: "Get a specific cookie by name for the attached tab's URL.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "Cookie URL scope. If omitted, uses attached tab URL.",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Cookie name.",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

func (t *cookiesGetTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	return executeTabTool(ctx, toolNameCookiesGet, rawArguments)
}

// executeTabTool is the shared execution flow for cookie tools.
// They follow the same broker pattern as http_request: register pending,
// broadcast event, wait for extension to respond.
func executeTabTool(ctx context.Context, toolName, rawArguments string) (string, error) {
	origin := runners.OriginFromContext(ctx)
	if origin != "webui" {
		channel := origin
		if channel == "" {
			channel = "automated"
		}
		return jsonError(fmt.Sprintf("%s is only supported on the webui channel, not %s", toolName, channel)), nil
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

	var args json.RawMessage
	if rawArguments != "" {
		args = json.RawMessage(rawArguments)
	} else {
		args = json.RawMessage("{}")
	}

	pending := &PendingToolCall{
		ID:             security.NewULID(),
		UserID:         user.ID,
		AgentID:        runner.AgentID,
		ConversationID: runner.ConversationID,
		ToolName:       toolName,
		Arguments:      args,
		resultChan:     MakeResultChan(),
	}
	broker.RegisterPending(pending)

	ps := pubsub.PubSubFromContext(ctx)
	if ps != nil {
		var parsedArgs interface{}
		json.Unmarshal(args, &parsedArgs)

		ps.Broadcast(pubsub.EventTypeTabToolCall, map[string]interface{}{
			"requestId":      pending.ID,
			"userId":         user.ID,
			"agentId":        runner.AgentID,
			"conversationId": runner.ConversationID,
			"toolName":       toolName,
			"arguments":      parsedArgs,
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
