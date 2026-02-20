package datetime

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/providers"
)

// RegisterTools adds the datetime tool to the registry.
func RegisterTools(registry *agents.ToolRegistry) {
	registry.Register(&datetimeTool{})
}

type datetimeTool struct{}

func (self *datetimeTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "datetime",
			Description: "Returns the current local date and time.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Returns: map[string]interface{}{
				"type":        "string",
				"description": "Current local date and time in YYYY-MM-DD HH:MM:SS TZ format.",
			},
		},
	}
}

func (self *datetimeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	return time.Now().Format("2006-01-02 15:04:05 MST"), nil
}
