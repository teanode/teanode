// Package surface exposes a tool that lets the agent render a schema-driven
// generative-UI surface (and optional interrupt) in the web UI.
package surface

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/integrations/surfaces"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/security"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&renderSurfaceTool{}, &closeSurfaceTool{}}
	})
}

type renderSurfaceTool struct{}

func (self *renderSurfaceTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "render_surface",
			Description: "Render an interactive, schema-driven UI surface directly in the web " +
				"chat — forms, choices, confirmations, buttons, tables, status, and dashboards. " +
				"Use this whenever the user asks for an interactive UI, or whenever a form, a set " +
				"of choices, a confirmation, or a structured/tabular view would serve the user " +
				"better than plain text. " +
				"IMPORTANT: displaying a surface REQUIRES calling this tool. Do NOT write a " +
				"```surface code block, and do NOT paste surface/component JSON into your reply — " +
				"that only shows raw text and renders nothing. Call this tool instead. " +
				"A surface is declarative and non-executable, built from this component catalog: " +
				"Section{title,children}, Markdown{text}, " +
				"KeyValueList{items:[{key,value}]}, Table{columns:[],rows:[[]]}, " +
				"StatusBadge{status,label} (status: success|warning|error|info|neutral), " +
				"ButtonRow{buttons:[{label,actionId,style,value}]}, " +
				"Form{fields:[{type,name,label,placeholder,options,required,defaultValue}],submitLabel,submitActionId} " +
				"(field type: TextInput|Textarea|Select|Checkbox), " +
				"CodeBlock{text,language}, Timeline{events:[{title,timestamp,description,status}]}. " +
				"Button clicks and form submissions are sent back to you as a user message, so you " +
				"can react to them. After a user acts on a surface it is closed automatically; to " +
				"update or replace a surface you already rendered, call this again with its " +
				"surfaceId. To dismiss a surface without replacing it, use close_surface. " +
				"Available only on the web UI channel; on other channels this tool returns an error, " +
				"so fall back to plain text there.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"surfaceId": map[string]interface{}{
						"type":        "string",
						"description": "Optional id of a surface you previously rendered, to replace/update it in place. Omit to create a new surface.",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Optional surface title shown in its header.",
					},
					"location": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"inline", "right_panel"},
						"description": "Where to render: inline in the conversation, or in the right side panel. Defaults to inline.",
					},
					"components": map[string]interface{}{
						"type":        "array",
						"description": "Ordered list of components to render. Each item has a \"type\" plus the fields for that type.",
						"items":       map[string]interface{}{"type": "object"},
					},
				},
				"required": []string{"components"},
			},
		},
	}
}

func (self *renderSurfaceTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *renderSurfaceTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	// Channel gate: surfaces only render on the web UI.
	origin := runners.OriginFromContext(ctx)
	if origin != runners.OriginWeb {
		channel := string(origin)
		if channel == "" {
			channel = "automated"
		}
		result, _ := json.Marshal(map[string]string{
			"error": fmt.Sprintf("render_surface is not supported on the %s channel", channel),
		})
		return string(result), nil
	}

	var arguments struct {
		SurfaceID  string                      `json:"surfaceId"`
		Title      string                      `json:"title"`
		Location   string                      `json:"location"`
		Components []surfaces.SurfaceComponent `json:"components"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("surface: parsing arguments: %w", err)
	}

	location := surfaces.SurfaceLocation(arguments.Location)
	if location == "" {
		location = surfaces.SurfaceLocationInline
	}

	broker := surfaces.SurfaceBrokerFromContext(ctx)
	if broker == nil {
		return "", fmt.Errorf("surface: surface broker not available")
	}
	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", fmt.Errorf("surface: runner context not available")
	}

	// Reuse the caller-supplied id to replace an existing surface in place, but
	// only if that surface belongs to this conversation — never overwrite a
	// surface registered for another conversation. Otherwise mint a new id.
	surfaceId := arguments.SurfaceID
	if surfaceId == "" {
		surfaceId = security.NewULID()
	} else if existing := broker.LookupSurface(surfaceId); existing != nil &&
		existing.ConversationID != "" && existing.ConversationID != runner.ConversationID {
		surfaceId = security.NewULID()
	}

	surface := &surfaces.Surface{
		SurfaceID:      surfaceId,
		SchemaVersion:  surfaces.SchemaVersion,
		Location:       location,
		Title:          arguments.Title,
		Components:     arguments.Components,
		ConversationID: runner.ConversationID,
		AgentID:        runner.AgentID,
		RunID:          runner.ID,
	}
	if err := surface.Validate(); err != nil {
		return "", err
	}

	broker.RegisterSurface(surface)

	if publisher := pubsub.PubSubFromContext(ctx); publisher != nil {
		event := map[string]interface{}{
			"action":         "emitted",
			"conversationId": surface.ConversationID,
			"agentId":        surface.AgentID,
			"runId":          surface.RunID,
			"surface":        surface,
		}
		// Scope the event to the owning user so it is not delivered to other
		// connected clients (shouldDeliverEvent filters on userId).
		if user := models.UserFromContext(ctx); user != nil {
			event["userId"] = user.ID
		}
		publisher.Broadcast(pubsub.EventTypeConversationSurfaces, event)
	}

	result, _ := json.Marshal(map[string]string{
		"status":    "rendered",
		"surfaceId": surface.SurfaceID,
	})
	return string(result), nil
}

// broadcastSurfaceRemoved notifies the owning user's clients to drop a surface.
func broadcastSurfaceRemoved(ctx context.Context, conversationId, surfaceId string) {
	publisher := pubsub.PubSubFromContext(ctx)
	if publisher == nil {
		return
	}
	event := map[string]interface{}{
		"action":         "removed",
		"conversationId": conversationId,
		"surfaceId":      surfaceId,
	}
	if user := models.UserFromContext(ctx); user != nil {
		event["userId"] = user.ID
	}
	publisher.Broadcast(pubsub.EventTypeConversationSurfaces, event)
}

type closeSurfaceTool struct{}

func (self *closeSurfaceTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "close_surface",
			Description: "Close (dismiss) a generative UI surface you previously rendered with " +
				"render_surface, removing it from the web chat. Pass the surfaceId returned by " +
				"render_surface to close one, or omit surfaceId to close all surfaces in this " +
				"conversation. Use this when a surface is no longer relevant. To update a surface " +
				"instead of closing it, call render_surface again with the same surfaceId. " +
				"Available only on the web UI channel.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"surfaceId": map[string]interface{}{
						"type":        "string",
						"description": "Id of the surface to close. Omit to close every surface in the conversation.",
					},
				},
			},
		},
	}
}

func (self *closeSurfaceTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *closeSurfaceTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	origin := runners.OriginFromContext(ctx)
	if origin != runners.OriginWeb {
		channel := string(origin)
		if channel == "" {
			channel = "automated"
		}
		result, _ := json.Marshal(map[string]string{
			"error": fmt.Sprintf("close_surface is not supported on the %s channel", channel),
		})
		return string(result), nil
	}

	var arguments struct {
		SurfaceID string `json:"surfaceId"`
	}
	if rawArguments != "" {
		if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
			return "", fmt.Errorf("surface: parsing arguments: %w", err)
		}
	}

	broker := surfaces.SurfaceBrokerFromContext(ctx)
	if broker == nil {
		return "", fmt.Errorf("surface: surface broker not available")
	}
	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", fmt.Errorf("surface: runner context not available")
	}
	conversationId := runner.ConversationID

	// Close a single surface (validated against this conversation) or all of the
	// conversation's surfaces when no id is supplied.
	var targets []*surfaces.Surface
	if arguments.SurfaceID != "" {
		surface := broker.LookupSurface(arguments.SurfaceID)
		if surface != nil && (surface.ConversationID == "" || surface.ConversationID == conversationId) {
			targets = append(targets, surface)
		}
	} else {
		targets = broker.SurfacesForConversation(conversationId)
	}

	for _, surface := range targets {
		broker.RemoveSurface(surface.SurfaceID)
		broadcastSurfaceRemoved(ctx, conversationId, surface.SurfaceID)
	}

	result, _ := json.Marshal(map[string]interface{}{
		"status": "closed",
		"closed": len(targets),
	})
	return string(result), nil
}
