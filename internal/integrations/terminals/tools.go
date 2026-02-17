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
	registry.Register(&terminalTool{relay: relay})
}

// resolveConnectionId returns the connectionId from args or falls back to the default.
func resolveConnectionId(relay *Relay, connectionId string) (string, error) {
	if connectionId != "" {
		return connectionId, nil
	}
	return relay.DefaultConnection()
}

// --- terminal (consolidated) ---

type terminalTool struct{ relay *Relay }

func (self *terminalTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "terminal",
			Description: "Control a connected terminal session. Actions: list (list connections), " +
				"screenshot (capture screen text), type (type text), press_key (press a special key).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "screenshot", "type", "press_key"},
						"description": "The terminal action to perform.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "ID of the terminal connection to target. If omitted, uses the default connection.",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to type into the terminal (for type action). Use \\n for newlines to execute commands.",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to press: Enter, Tab, Escape, Backspace, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Ctrl+C, Ctrl+D, Ctrl+Z, Ctrl+L (for press_key action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent result. list: {connections: [...]}. screenshot: {text}. type: {text}. press_key: {key}.",
			},
		},
	}
}

func (self *terminalTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action       string `json:"action"`
		ConnectionID string `json:"connectionId"`
		Text         string `json:"text"`
		Key          string `json:"key"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		return self.executeList()
	case "screenshot":
		return self.executeScreenshot(ctx, arguments.ConnectionID)
	case "type":
		return self.executeType(ctx, arguments.ConnectionID, arguments.Text)
	case "press_key":
		return self.executePressKey(ctx, arguments.ConnectionID, arguments.Key)
	default:
		return "", fmt.Errorf("unknown terminal action: %s", arguments.Action)
	}
}

func (self *terminalTool) executeList() (string, error) {
	connections := self.relay.Connections()
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
	output, _ := json.Marshal(map[string]interface{}{"connections": entries})
	return string(output), nil
}

func (self *terminalTool) executeScreenshot(ctx context.Context, connectionId string) (string, error) {
	resolvedId, err := resolveConnectionId(self.relay, connectionId)
	if err != nil {
		return "", err
	}
	result, err := self.relay.SendCommand(ctx, resolvedId, "screenshot", nil)
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

func (self *terminalTool) executeType(ctx context.Context, connectionId string, text string) (string, error) {
	resolvedId, err := resolveConnectionId(self.relay, connectionId)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCommand(ctx, resolvedId, "write", map[string]interface{}{
		"data": text,
	})
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"text": text})
	return string(output), nil
}

func (self *terminalTool) executePressKey(ctx context.Context, connectionId string, key string) (string, error) {
	seq, ok := termKeyMap[strings.ToLower(key)]
	if !ok {
		return "", fmt.Errorf("unknown key: %s", key)
	}
	resolvedId, err := resolveConnectionId(self.relay, connectionId)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCommand(ctx, resolvedId, "write", map[string]interface{}{
		"data": seq,
	})
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"key": key})
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
