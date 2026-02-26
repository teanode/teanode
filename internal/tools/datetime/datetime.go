package datetime

import (
	"context"
	"time"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&datetimeTool{}}
	})
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
