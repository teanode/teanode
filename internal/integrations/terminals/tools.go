package terminals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterTerminalTools adds all terminal-control tools to the registry.
func RegisterTerminalTools(registry *agents.ToolRegistry, relay *Relay) {
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
			Description: "List all connected terminal connections.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Returns: map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":               map[string]interface{}{"type": "string", "description": "Connection identifier"},
						"hostname":         map[string]interface{}{"type": "string", "description": "Machine hostname"},
						"username":         map[string]interface{}{"type": "string", "description": "OS username"},
						"os":               map[string]interface{}{"type": "string", "description": "Operating system (linux, darwin, windows)"},
						"architecture":     map[string]interface{}{"type": "string", "description": "CPU architecture (amd64, arm64)"},
						"shellCommand":     map[string]interface{}{"type": "string", "description": "Shell command running in the terminal"},
						"workingDirectory": map[string]interface{}{"type": "string", "description": "Working directory of the terminal"},
						"timezone":         map[string]interface{}{"type": "string", "description": "IANA timezone (e.g. America/New_York)"},
					},
				},
			},
		},
	}
}

func (self *terminalConnectionListTool) Execute(_ context.Context, _ string) (string, error) {
	connections := self.relay.Connections()
	if len(connections) == 0 {
		return "[]", nil
	}
	type entry struct {
		ID               string `json:"id"`
		Hostname         string `json:"hostname,omitempty"`
		Username         string `json:"username,omitempty"`
		OS               string `json:"os,omitempty"`
		Architecture     string `json:"architecture,omitempty"`
		ShellCommand     string `json:"shellCommand,omitempty"`
		WorkingDirectory string `json:"workingDirectory,omitempty"`
		Timezone         string `json:"timezone,omitempty"`
	}
	entries := make([]entry, len(connections))
	for index, connection := range connections {
		entries[index] = entry{
			ID:               connection.ID,
			Hostname:         connection.Machine.Hostname,
			Username:         connection.Machine.Username,
			OS:               connection.Machine.OS,
			Architecture:     connection.Machine.Architecture,
			ShellCommand:     connection.Machine.ShellCommand,
			WorkingDirectory: connection.Machine.WorkingDirectory,
			Timezone:         connection.Machine.Timezone,
		}
	}
	out, _ := json.Marshal(entries)
	return string(out), nil
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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{"type": "string", "description": "Plain text content of the terminal screen"},
				},
			},
		},
	}
}

func (self *terminalScreenshotTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		ConnectionID string `json:"connectionId"`
	}
	if rawArguments != "" {
		json.Unmarshal([]byte(rawArguments), &arguments)
	}
	connectionId, err := resolveConnectionId(self.relay, arguments.ConnectionID)
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
	output, _ := json.Marshal(map[string]string{"text": response.Text})
	return string(output), nil
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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{"type": "string", "description": "The text that was typed"},
				},
			},
		},
	}
}

func (self *terminalTypeTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Text         string `json:"text"`
		ConnectionID string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	connectionId, err := resolveConnectionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCommand(ctx, connectionId, "write", map[string]interface{}{
		"data": arguments.Text,
	})
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"text": arguments.Text})
	return string(output), nil
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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{"type": "string", "description": "The key that was pressed"},
				},
			},
		},
	}
}

func (self *terminalPressKeyTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Key          string `json:"key"`
		ConnectionID string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	seq, ok := termKeyMap[strings.ToLower(arguments.Key)]
	if !ok {
		return "", fmt.Errorf("unknown key: %s", arguments.Key)
	}

	connectionId, err := resolveConnectionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCommand(ctx, connectionId, "write", map[string]interface{}{
		"data": seq,
	})
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"key": arguments.Key})
	return string(output), nil
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
