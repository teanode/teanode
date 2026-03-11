package datetime

import (
	"context"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

// clock returns the current time. Overridden in tests for determinism.
var clock = time.Now

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
			Description: "Returns the current local date and time. Note: the current datetime is already provided automatically in the conversation context, so calling this tool is rarely necessary.",
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

// formatNow returns the current datetime as a formatted string.
// Both Execute and BuildOverlay share this code path to avoid drift.
func formatNow() string {
	return clock().Format("2006-01-02 15:04:05 MST")
}

func (self *datetimeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	return formatNow(), nil
}

// BuildOverlay implements tools.OverlayBuilder. It injects the current datetime
// and timezone into the prompt context on every turn.
func (self *datetimeTool) BuildOverlay(ctx context.Context) (string, error) {
	return fmt.Sprintf("<current_datetime>\n%s\n</current_datetime>", formatNow()), nil
}
