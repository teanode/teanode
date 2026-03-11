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

func (self *datetimeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	return clock().Format("2006-01-02 15:04:05 MST"), nil
}

// BuildOverlay implements tools.OverlayBuilder. It injects the current datetime
// and timezone into the prompt context on every turn.
func (self *datetimeTool) BuildOverlay(ctx context.Context) (string, error) {
	now := clock()
	_, offset := now.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60

	return fmt.Sprintf("<current_datetime>\n%s (UTC%s%02d:%02d)\n</current_datetime>",
		now.Format("2006-01-02 15:04:05 MST"),
		sign, hours, minutes,
	), nil
}
