package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/provider"
)

// RegisterBrowserTools adds all browser-control tools to the registry.
func RegisterBrowserTools(registry *agent.ToolRegistry, relay *Relay) {
	registry.Register(&browserNavigateTool{relay: relay})
	registry.Register(&browserScreenshotTool{relay: relay})
	registry.Register(&browserSnapshotTool{relay: relay})
	registry.Register(&browserClickTool{relay: relay})
	registry.Register(&browserTypeTool{relay: relay})
	registry.Register(&browserPressKeyTool{relay: relay})
	registry.Register(&browserEvaluateTool{relay: relay})
	registry.Register(&browserTabListTool{relay: relay})
	registry.Register(&browserTabOpenTool{relay: relay})
	registry.Register(&browserTabCloseTool{relay: relay})
	registry.Register(&browserTabActivateTool{relay: relay})
}

// resolveSessionId returns the sessionId for the given connectionId,
// or falls back to the default target's session ID.
func resolveSessionId(relay *Relay, connectionId string) (string, error) {
	if connectionId != "" {
		target, err := relay.TargetByConnectionId(connectionId)
		if err != nil {
			return "", err
		}
		return target.SessionID, nil
	}
	target, err := relay.DefaultTarget()
	if err != nil {
		return "", err
	}
	return target.SessionID, nil
}

// --- browser_navigate ---

type browserNavigateTool struct{ relay *Relay }

func (self *browserNavigateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_navigate",
			Description: "Navigate the browser to a URL.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to navigate to.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (self *browserNavigateTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		URL          string `json:"url"`
		ConnectionID string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(input), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCDPCommand(ctx, "Page.navigate", map[string]interface{}{
		"url": arguments.URL,
	}, sessionId)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Navigated to %s", arguments.URL), nil
}

// --- browser_screenshot ---

type browserScreenshotTool struct{ relay *Relay }

func (self *browserScreenshotTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_screenshot",
			Description: "Take a screenshot of the current browser tab. Returns a base64-encoded PNG image.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
			},
		},
	}
}

func (self *browserScreenshotTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		ConnectionID string `json:"connectionId"`
	}
	if input != "" {
		json.Unmarshal([]byte(input), &arguments)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	result, err := self.relay.SendCDPCommand(ctx, "Page.captureScreenshot", map[string]interface{}{
		"format": "png",
	}, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("parsing screenshot: %w", err)
	}
	out, _ := json.Marshal(map[string]string{
		"base64": response.Data,
		"format": "png",
	})
	return string(out), nil
}

// --- browser_snapshot ---

type browserSnapshotTool struct{ relay *Relay }

func (self *browserSnapshotTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_snapshot",
			Description: "Get the accessibility tree of the current page. This is the primary way to understand page structure and content.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
			},
		},
	}
}

func (self *browserSnapshotTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		ConnectionID string `json:"connectionId"`
	}
	if input != "" {
		json.Unmarshal([]byte(input), &arguments)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	result, err := self.relay.SendCDPCommand(ctx, "Accessibility.getFullAXTree", nil, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		Nodes []accessibilityNode `json:"nodes"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("parsing accessibility tree: %w", err)
	}
	return buildAXTree(response.Nodes), nil
}

type accessibilityNode struct {
	NodeID     string      `json:"nodeId"`
	ParentID   string      `json:"parentId"`
	Role       accessibilityValue     `json:"role"`
	Name       accessibilityValue     `json:"name"`
	Value      *accessibilityValue    `json:"value"`
	Properties []accessibilityProperty    `json:"properties"`
	ChildIDs   []string    `json:"childIds"`
	Ignored    bool        `json:"ignored"`
}

type accessibilityValue struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

type accessibilityProperty struct {
	Name  string  `json:"name"`
	Value accessibilityValue `json:"value"`
}

func buildAXTree(nodes []accessibilityNode) string {
	if len(nodes) == 0 {
		return "(empty accessibility tree)"
	}

	nodesByID := make(map[string]*accessibilityNode, len(nodes))
	for i := range nodes {
		nodesByID[nodes[i].NodeID] = &nodes[i]
	}

	var builder strings.Builder
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		node, ok := nodesByID[id]
		if !ok || node.Ignored {
			return
		}

		role := fmt.Sprintf("%v", node.Role.Value)
		name := fmt.Sprintf("%v", node.Name.Value)

		// Skip generic/none roles without meaningful names.
		if (role == "none" || role == "generic" || role == "") && name == "" {
			for _, childID := range node.ChildIDs {
				walk(childID, depth)
			}
			return
		}

		indent := strings.Repeat("  ", depth)
		line := indent + role
		if name != "" {
			line += fmt.Sprintf(" %q", name)
		}

		// Add notable properties.
		for _, property := range node.Properties {
			switch property.Name {
			case "level":
				line += fmt.Sprintf(" (level %v)", property.Value.Value)
			case "checked":
				line += fmt.Sprintf(" checked=%v", property.Value.Value)
			case "disabled":
				if property.Value.Value == true {
					line += " disabled"
				}
			}
		}
		if node.Value != nil && node.Value.Value != nil {
			line += fmt.Sprintf(" value=%q", fmt.Sprintf("%v", node.Value.Value))
		}

		builder.WriteString(line)
		builder.WriteByte('\n')

		for _, childID := range node.ChildIDs {
			walk(childID, depth+1)
		}
	}

	// Start from the root (first node).
	walk(nodes[0].NodeID, 0)
	return strings.TrimRight(builder.String(), "\n")
}

// --- browser_click ---

type browserClickTool struct{ relay *Relay }

func (self *browserClickTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_click",
			Description: "Click an element on the page. Provide either (x, y) coordinates or a CSS selector.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"x": map[string]interface{}{
						"type":        "number",
						"description": "X coordinate to click.",
					},
					"y": map[string]interface{}{
						"type":        "number",
						"description": "Y coordinate to click.",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element to click.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
			},
		},
	}
}

func (self *browserClickTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		X            *float64 `json:"x"`
		Y            *float64 `json:"y"`
		Selector     string   `json:"selector"`
		ConnectionID string   `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(input), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}

	if arguments.Selector != "" {
		expr := fmt.Sprintf(`document.querySelector(%q)?.click()`, arguments.Selector)
		_, err := self.relay.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Clicked selector %q", arguments.Selector), nil
	}

	if arguments.X == nil || arguments.Y == nil {
		return "", fmt.Errorf("provide either (x, y) coordinates or a selector")
	}
	x, y := *arguments.X, *arguments.Y
	for _, evType := range []string{"mousePressed", "mouseReleased"} {
		_, err := self.relay.SendCDPCommand(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
			"type":       evType,
			"x":         x,
			"y":         y,
			"button":    "left",
			"clickCount": 1,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Clicked at (%.0f, %.0f)", x, y), nil
}

// --- browser_type ---

type browserTypeTool struct{ relay *Relay }

func (self *browserTypeTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_type",
			Description: "Type text into the focused element or an element matching a CSS selector.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to type.",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "Optional CSS selector to focus before typing.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
				"required": []string{"text"},
			},
		},
	}
}

func (self *browserTypeTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		Text         string `json:"text"`
		Selector     string `json:"selector"`
		ConnectionID string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(input), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}

	if arguments.Selector != "" {
		expr := fmt.Sprintf(`document.querySelector(%q)?.focus()`, arguments.Selector)
		_, err := self.relay.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}

	_, err = self.relay.SendCDPCommand(ctx, "Input.insertText", map[string]interface{}{
		"text": arguments.Text,
	}, sessionId)
	if err != nil {
		return "", err
	}
	return "ok", nil
}

// --- browser_press_key ---

type browserPressKeyTool struct{ relay *Relay }

func (self *browserPressKeyTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_press_key",
			Description: "Press a keyboard key (e.g. \"Enter\", \"Tab\", \"Escape\", \"ArrowDown\").",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to press (DOM key value).",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
				"required": []string{"key"},
			},
		},
	}
}

func (self *browserPressKeyTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		Key          string `json:"key"`
		ConnectionID string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(input), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}

	info := keyInfo(arguments.Key)

	for _, evType := range []string{"keyDown", "keyUp"} {
		params := map[string]interface{}{
			"type": evType,
			"key":  info.key,
		}
		if info.code != "" {
			params["code"] = info.code
		}
		if info.keyCode != 0 {
			params["windowsVirtualKeyCode"] = info.keyCode
			params["nativeVirtualKeyCode"] = info.keyCode
		}
		if evType == "keyDown" && len(info.text) > 0 {
			params["text"] = info.text
		}
		_, err := self.relay.SendCDPCommand(ctx, "Input.dispatchKeyEvent", params, sessionId)
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Pressed %s", arguments.Key), nil
}

type keyData struct {
	key     string
	code    string
	keyCode int
	text    string
}

func keyInfo(key string) keyData {
	switch key {
	case "Enter":
		return keyData{key: "Enter", code: "Enter", keyCode: 13, text: "\r"}
	case "Tab":
		return keyData{key: "Tab", code: "Tab", keyCode: 9, text: "\t"}
	case "Backspace":
		return keyData{key: "Backspace", code: "Backspace", keyCode: 8}
	case "Delete":
		return keyData{key: "Delete", code: "Delete", keyCode: 46}
	case "Escape":
		return keyData{key: "Escape", code: "Escape", keyCode: 27}
	case "ArrowUp":
		return keyData{key: "ArrowUp", code: "ArrowUp", keyCode: 38}
	case "ArrowDown":
		return keyData{key: "ArrowDown", code: "ArrowDown", keyCode: 40}
	case "ArrowLeft":
		return keyData{key: "ArrowLeft", code: "ArrowLeft", keyCode: 37}
	case "ArrowRight":
		return keyData{key: "ArrowRight", code: "ArrowRight", keyCode: 39}
	case "Home":
		return keyData{key: "Home", code: "Home", keyCode: 36}
	case "End":
		return keyData{key: "End", code: "End", keyCode: 35}
	case "PageUp":
		return keyData{key: "PageUp", code: "PageUp", keyCode: 33}
	case "PageDown":
		return keyData{key: "PageDown", code: "PageDown", keyCode: 34}
	case " ":
		return keyData{key: " ", code: "Space", keyCode: 32, text: " "}
	default:
		return keyData{key: key}
	}
}

// --- browser_evaluate ---

type browserEvaluateTool struct{ relay *Relay }

func (self *browserEvaluateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_evaluate",
			Description: "Evaluate a JavaScript expression in the browser and return the result.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript expression to evaluate.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
				"required": []string{"expression"},
			},
		},
	}
}

func (self *browserEvaluateTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		Expression   string `json:"expression"`
		ConnectionID string `json:"connectionId"`
	}
	if err := json.Unmarshal([]byte(input), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	sessionId, err := resolveSessionId(self.relay, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	result, err := self.relay.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    arguments.Expression,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return string(result), nil
	}
	if response.ExceptionDetails != nil {
		return "", fmt.Errorf("evaluation error: %s", response.ExceptionDetails.Text)
	}
	if response.Result.Value != nil {
		return string(response.Result.Value), nil
	}
	return response.Result.Type, nil
}

// --- browser_tab_list ---

type browserTabListTool struct{ relay *Relay }

func (self *browserTabListTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_tab_list",
			Description: "List all attached browser tabs.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

func (self *browserTabListTool) Execute(_ context.Context, _ string) (string, error) {
	targets := self.relay.Targets()
	if len(targets) == 0 {
		return "No attached tabs.", nil
	}
	var builder strings.Builder
	for _, target := range targets {
		fmt.Fprintf(&builder, "- [%s] %s (%s) connectionId=%s\n", target.TargetID, target.Title, target.URL, target.SessionID)
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

// --- browser_tab_open ---

type browserTabOpenTool struct{ relay *Relay }

func (self *browserTabOpenTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_tab_open",
			Description: "Open a new browser tab, optionally navigating to a URL.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to open in the new tab. Defaults to about:blank.",
					},
				},
			},
		},
	}
}

func (self *browserTabOpenTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		URL string `json:"url"`
	}
	if input != "" {
		json.Unmarshal([]byte(input), &arguments)
	}
	if arguments.URL == "" {
		arguments.URL = "about:blank"
	}

	sessionId, err := resolveSessionId(self.relay, "")
	if err != nil {
		return "", err
	}
	result, err := self.relay.SendCDPCommand(ctx, "Target.createTarget", map[string]interface{}{
		"url": arguments.URL,
	}, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		TargetID string `json:"targetId"`
	}
	if json.Unmarshal(result, &response) == nil && response.TargetID != "" {
		return fmt.Sprintf("Opened new tab %s at %s", response.TargetID, arguments.URL), nil
	}
	return fmt.Sprintf("Opened new tab at %s", arguments.URL), nil
}

// --- browser_tab_close ---

type browserTabCloseTool struct{ relay *Relay }

func (self *browserTabCloseTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_tab_close",
			Description: "Close a browser tab by target ID. Closes the current tab if no ID is given.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"targetId": map[string]interface{}{
						"type":        "string",
						"description": "Target ID of the tab to close.",
					},
				},
			},
		},
	}
}

func (self *browserTabCloseTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		TargetID string `json:"targetId"`
	}
	if input != "" {
		json.Unmarshal([]byte(input), &arguments)
	}

	sessionId, err := resolveSessionId(self.relay, "")
	if err != nil {
		return "", err
	}
	params := map[string]interface{}{}
	if arguments.TargetID != "" {
		params["targetId"] = arguments.TargetID
	}
	_, err = self.relay.SendCDPCommand(ctx, "Target.closeTarget", params, sessionId)
	if err != nil {
		return "", err
	}
	return "Tab closed.", nil
}

// --- browser_tab_activate ---

type browserTabActivateTool struct{ relay *Relay }

func (self *browserTabActivateTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_tab_activate",
			Description: "Bring a browser tab to the foreground by target ID.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"targetId": map[string]interface{}{
						"type":        "string",
						"description": "Target ID of the tab to activate.",
					},
				},
				"required": []string{"targetId"},
			},
		},
	}
}

func (self *browserTabActivateTool) Execute(ctx context.Context, input string) (string, error) {
	var arguments struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal([]byte(input), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	sessionId, err := resolveSessionId(self.relay, "")
	if err != nil {
		return "", err
	}
	_, err = self.relay.SendCDPCommand(ctx, "Target.activateTarget", map[string]interface{}{
		"targetId": arguments.TargetID,
	}, sessionId)
	if err != nil {
		return "", err
	}
	return "Tab activated.", nil
}
