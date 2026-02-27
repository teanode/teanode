package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/integrations/terminals"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&terminalTool{}}
	})
}

// resolveConnectionId returns the connectionId from args or falls back to the user's default.
func resolveConnectionId(relay *terminals.Relay, userId, connectionId string) (string, error) {
	if connectionId != "" {
		return connectionId, nil
	}
	return relay.DefaultConnectionForUser(userId)
}

// --- terminal (consolidated) ---

type terminalTool struct{}

func (self *terminalTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
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
	relay := terminals.TerminalFromContext(ctx)
	if relay == nil {
		return "", fmt.Errorf("no terminal relay available")
	}

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
		return executeList(ctx, relay)
	case "screenshot":
		return executeScreenshot(ctx, relay, arguments.ConnectionID)
	case "type":
		return executeTypeAction(ctx, relay, arguments.ConnectionID, arguments.Text)
	case "press_key":
		return executePressKey(ctx, relay, arguments.ConnectionID, arguments.Key)
	default:
		return "", fmt.Errorf("unknown terminal action: %s", arguments.Action)
	}
}

func executeList(ctx context.Context, relay *terminals.Relay) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	connections := relay.ConnectionsForUser(user.ID)
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

func executeScreenshot(ctx context.Context, relay *terminals.Relay, connectionId string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	resolvedId, err := resolveConnectionId(relay, user.ID, connectionId)
	if err != nil {
		return "", err
	}
	result, err := relay.SendCommandForUser(ctx, user.ID, resolvedId, "screenshot", nil)
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

func executeTypeAction(ctx context.Context, relay *terminals.Relay, connectionId string, text string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	resolvedId, err := resolveConnectionId(relay, user.ID, connectionId)
	if err != nil {
		return "", err
	}
	_, err = relay.SendCommandForUser(ctx, user.ID, resolvedId, "write", map[string]interface{}{
		"data": text,
	})
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"text": text})
	return string(output), nil
}

func executePressKey(ctx context.Context, relay *terminals.Relay, connectionId string, key string) (string, error) {
	sequence, ok := termKeyMap[strings.ToLower(key)]
	if !ok {
		return "", fmt.Errorf("unknown key: %s", key)
	}
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	resolvedId, err := resolveConnectionId(relay, user.ID, connectionId)
	if err != nil {
		return "", err
	}
	_, err = relay.SendCommandForUser(ctx, user.ID, resolvedId, "write", map[string]interface{}{
		"data": sequence,
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
