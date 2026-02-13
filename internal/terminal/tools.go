package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/provider"
)

// RegisterTerminalTools adds all terminal-control tools to the registry.
func RegisterTerminalTools(registry *agent.ToolRegistry, relay *Relay) {
	registry.Register(&terminalConnectionListTool{relay: relay})
	registry.Register(&terminalScreenshotTool{relay: relay})
	registry.Register(&terminalTypeTool{relay: relay})
	registry.Register(&terminalPressKeyTool{relay: relay})
}

// resolveConnectionId returns the connectionId from args or falls back to the default.
func resolveConnectionId(relay *Relay, connectionId string) (string, error) {
	if connectionId != "" {
		return connectionId, nil
	}
	return relay.DefaultConnection()
}

// --- terminal_connection_list ---

type terminalConnectionListTool struct{ relay *Relay }

func (self *terminalConnectionListTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "terminal_connection_list",
			Description: "List all connected terminal connections by ID.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (self *terminalConnectionListTool) Execute(_ context.Context, _ string) (string, error) {
	connections := self.relay.Connections()
	if len(connections) == 0 {
		return "No connected terminals.", nil
	}
	var builder strings.Builder
	for _, connection := range connections {
		fmt.Fprintf(&builder, "- %s\n", connection.ID)
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

// --- terminal_screenshot ---

type terminalScreenshotTool struct{ relay *Relay }

func (self *terminalScreenshotTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "terminal_screenshot",
			Description: "Capture the current terminal screen content as plain text.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the terminal connection to target. If omitted, uses the default connection.",
					},
				},
			},
		},
	}
}

func (self *terminalScreenshotTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		ConnectionId string `json:"connectionId"`
	}
	if rawArguments != "" {
		json.Unmarshal([]byte(rawArguments), &arguments)
	}
	connectionId, err := resolveConnectionId(self.relay, arguments.ConnectionId)
	if err != nil {
		return "", err
	}
	result, err := self.relay.SendCommand(ctx, connectionId, "screenshot", nil)
	if err != nil {
		return "", err
	}
	var response struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("parsing screenshot: %w", err)
	}
	return response.Text, nil
}

// --- terminal_type ---

type terminalTypeTool struct{ relay *Relay }

func (self *terminalTypeTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "terminal_type",
			Description: "Type text into the terminal. Use \\n for newlines to execute commands.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to type into the terminal.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the terminal connection to target. If omitted, uses the default connection.",
					},
				},
				"required": []string{"text"},
			},
		},
	}
}

func (self *terminalTypeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Text         string `json:"text"`
		ConnectionId string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	connectionId, err := resolveConnectionId(self.relay, arguments.ConnectionId)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCommand(ctx, connectionId, "write", map[string]interface{}{
		"data": arguments.Text,
	})
	if err != nil {
		return "", err
	}
	return "ok", nil
}

// --- terminal_press_key ---

type terminalPressKeyTool struct{ relay *Relay }

func (self *terminalPressKeyTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "terminal_press_key",
			Description: "Press a special key or key combination in the terminal (e.g. \"Enter\", \"Ctrl+C\", \"ArrowUp\").",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to press (Enter, Tab, Escape, Backspace, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+L).",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the terminal connection to target. If omitted, uses the default connection.",
					},
				},
				"required": []string{"key"},
			},
		},
	}
}

func (self *terminalPressKeyTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Key          string `json:"key"`
		ConnectionId string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	seq, ok := termKeyMap[strings.ToLower(arguments.Key)]
	if !ok {
		return "", fmt.Errorf("unknown key: %s", arguments.Key)
	}

	connectionId, err := resolveConnectionId(self.relay, arguments.ConnectionId)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCommand(ctx, connectionId, "write", map[string]interface{}{
		"data": seq,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Pressed %s", arguments.Key), nil
}

// termKeyMap maps key names (lowercase) to their escape sequences.
var termKeyMap = map[string]string{
	"enter":      "\r",
	"tab":        "\t",
	"escape":     "\x1b",
	"backspace":  "\x7f",
	"arrowup":    "\x1b[A",
	"arrowdown":  "\x1b[B",
	"arrowright": "\x1b[C",
	"arrowleft":  "\x1b[D",
	"ctrl+c":     "\x03",
	"ctrl+d":     "\x04",
	"ctrl+z":     "\x1a",
	"ctrl+l":     "\x0c",
}
