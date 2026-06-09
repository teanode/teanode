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
		return []tools.Tool{&renderSurfaceTool{}}
	})
}

type renderSurfaceTool struct{}

func (self *renderSurfaceTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "render_surface",
			Description: "Render a schema-driven generative UI surface in the web app. " +
				"A surface is a declarative, non-executable fragment built from a small " +
				"component catalog. Only works on the web UI channel. " +
				"Component types: Section{title,children}, Markdown{text}, " +
				"KeyValueList{items:[{key,value}]}, Table{columns:[],rows:[[]]}, " +
				"StatusBadge{status,label} (status: success|warning|error|info|neutral), " +
				"ButtonRow{buttons:[{label,actionId,style,value}]}, " +
				"Form{fields:[{type,name,label,placeholder,options,required,defaultValue}],submitLabel,submitActionId} " +
				"(field type: TextInput|Textarea|Select|Checkbox), " +
				"CodeBlock{text,language}, Timeline{events:[{title,timestamp,description,status}]}. " +
				"Button and form submissions are sent back to you as a user message.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
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

	surface := &surfaces.Surface{
		SurfaceID:      security.NewULID(),
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
		publisher.Broadcast(pubsub.EventTypeConversationSurfaces, map[string]interface{}{
			"action":         "emitted",
			"conversationId": surface.ConversationID,
			"agentId":        surface.AgentID,
			"runId":          surface.RunID,
			"surface":        surface,
		})
	}

	result, _ := json.Marshal(map[string]string{
		"status":    "rendered",
		"surfaceId": surface.SurfaceID,
	})
	return string(result), nil
}
