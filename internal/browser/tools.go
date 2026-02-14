package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/agent"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterBrowserTools adds all browser-control tools to the registry.
func RegisterBrowserTools(registry *agent.ToolRegistry, browser Browser) {
	registry.Register(&browserNavigateTool{browser: browser})
	registry.Register(&browserScreenshotTool{browser: browser})
	registry.Register(&browserSnapshotTool{browser: browser})
	registry.Register(&browserClickTool{browser: browser})
	registry.Register(&browserTypeTool{browser: browser})
	registry.Register(&browserPressKeyTool{browser: browser})
	registry.Register(&browserEvaluateTool{browser: browser})
	registry.Register(&browserTabListTool{browser: browser})
	registry.Register(&browserTabOpenTool{browser: browser})
	registry.Register(&browserTabCloseTool{browser: browser})
	registry.Register(&browserTabActivateTool{browser: browser})
}

// resolveSessionId returns the sessionId for the given connectionId,
// or falls back to the default target's session ID.
func resolveSessionId(browser Browser, connectionId string) (string, error) {
	if connectionId != "" {
		target, err := browser.TargetByConnectionId(connectionId)
		if err != nil {
			return "", err
		}
		return target.SessionID, nil
	}
	target, err := browser.DefaultTarget()
	if err != nil {
		return "", err
	}
	return target.SessionID, nil
}

// --- browser_navigate ---

type browserNavigateTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{"type": "string"},
				},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	_, err = self.browser.SendCDPCommand(ctx, "Page.navigate", map[string]interface{}{
		"url": arguments.URL,
	}, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"url": arguments.URL})
	return string(out), nil
}

// --- browser_screenshot ---

type browserScreenshotTool struct{ browser Browser }

func (self *browserScreenshotTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "browser_screenshot",
			Description: "Take a screenshot of the current browser tab. The screenshot is displayed directly to the user in the chat.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Session ID of the browser tab to target. If omitted, uses the default tab.",
					},
				},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mediaId":   map[string]interface{}{"type": "string", "description": "Unique identifier for the saved media file"},
					"format":    map[string]interface{}{"type": "string", "description": "Image format (png)"},
					"displayed": map[string]interface{}{"type": "boolean", "description": "Whether the image was displayed to the user"},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	result, err := self.browser.SendCDPCommand(ctx, "Page.captureScreenshot", map[string]interface{}{
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

type browserSnapshotTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"tree": map[string]interface{}{"type": "string", "description": "Indented text representation of the accessibility tree"},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	result, err := self.browser.SendCDPCommand(ctx, "Accessibility.getFullAXTree", nil, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		Nodes []accessibilityNode `json:"nodes"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("parsing accessibility tree: %w", err)
	}
	out, _ := json.Marshal(map[string]string{"tree": buildAXTree(response.Nodes)})
	return string(out), nil
}

type accessibilityNode struct {
	NodeID     string                  `json:"nodeId"`
	ParentID   string                  `json:"parentId"`
	Role       accessibilityValue      `json:"role"`
	Name       accessibilityValue      `json:"name"`
	Value      *accessibilityValue     `json:"value"`
	Properties []accessibilityProperty `json:"properties"`
	ChildIDs   []string                `json:"childIds"`
	Ignored    bool                    `json:"ignored"`
}

type accessibilityValue struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

type accessibilityProperty struct {
	Name  string             `json:"name"`
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

type browserClickTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{"type": "string", "description": "CSS selector that was clicked (if selector was used)"},
					"x":        map[string]interface{}{"type": "number", "description": "X coordinate clicked (if coordinates were used)"},
					"y":        map[string]interface{}{"type": "number", "description": "Y coordinate clicked (if coordinates were used)"},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
	if err != nil {
		return "", err
	}

	if arguments.Selector != "" {
		expr := fmt.Sprintf(`document.querySelector(%q)?.click()`, arguments.Selector)
		_, err := self.browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(map[string]string{"selector": arguments.Selector})
		return string(out), nil
	}

	if arguments.X == nil || arguments.Y == nil {
		return "", fmt.Errorf("provide either (x, y) coordinates or a selector")
	}
	x, y := *arguments.X, *arguments.Y
	for _, evType := range []string{"mousePressed", "mouseReleased"} {
		_, err := self.browser.SendCDPCommand(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
			"type":       evType,
			"x":          x,
			"y":          y,
			"button":     "left",
			"clickCount": 1,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}
	out, _ := json.Marshal(map[string]interface{}{"x": x, "y": y})
	return string(out), nil
}

// --- browser_type ---

type browserTypeTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{"type": "string", "description": "The text that was typed"},
				},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
	if err != nil {
		return "", err
	}

	if arguments.Selector != "" {
		expr := fmt.Sprintf(`document.querySelector(%q)?.focus()`, arguments.Selector)
		_, err := self.browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}

	_, err = self.browser.SendCDPCommand(ctx, "Input.insertText", map[string]interface{}{
		"text": arguments.Text,
	}, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"text": arguments.Text})
	return string(out), nil
}

// --- browser_press_key ---

type browserPressKeyTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{"type": "string", "description": "The key that was pressed"},
				},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
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
		_, err := self.browser.SendCDPCommand(ctx, "Input.dispatchKeyEvent", params, sessionId)
		if err != nil {
			return "", err
		}
	}
	out, _ := json.Marshal(map[string]string{"key": arguments.Key})
	return string(out), nil
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

type browserEvaluateTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type":  map[string]interface{}{"type": "string", "description": "JavaScript type of the result (string, number, boolean, object, undefined)"},
					"value": map[string]interface{}{"description": "The evaluated result value"},
				},
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
	sessionId, err := resolveSessionId(self.browser, arguments.ConnectionID)
	if err != nil {
		return "", err
	}
	result, err := self.browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
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
	out, _ := json.Marshal(map[string]interface{}{
		"type":  response.Result.Type,
		"value": response.Result.Value,
	})
	return string(out), nil
}

// --- browser_tab_list ---

type browserTabListTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"targetId":     map[string]interface{}{"type": "string", "description": "Unique target identifier"},
						"title":        map[string]interface{}{"type": "string", "description": "Page title"},
						"url":          map[string]interface{}{"type": "string", "description": "Page URL"},
						"connectionId": map[string]interface{}{"type": "string", "description": "Session ID used as connectionId for other tools"},
						"source":       map[string]interface{}{"type": "string", "description": "Browser backend hosting this tab (headless or extension)"},
					},
				},
			},
		},
	}
}

func (self *browserTabListTool) Execute(_ context.Context, _ string) (string, error) {
	targets := self.browser.Targets()
	type entry struct {
		TargetID     string `json:"targetId"`
		Title        string `json:"title"`
		URL          string `json:"url"`
		ConnectionID string `json:"connectionId"`
		Source       string `json:"source"`
	}
	entries := make([]entry, len(targets))
	for index, target := range targets {
		entries[index] = entry{
			TargetID:     target.TargetID,
			Title:        target.Title,
			URL:          target.URL,
			ConnectionID: target.SessionID,
			Source:       target.Source,
		}
	}
	out, _ := json.Marshal(entries)
	return string(out), nil
}

// --- browser_tab_open ---

type browserTabOpenTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"targetId": map[string]interface{}{"type": "string", "description": "Target ID of the new tab"},
					"url":      map[string]interface{}{"type": "string", "description": "URL opened in the new tab"},
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

	sessionId, err := resolveSessionId(self.browser, "")
	if err != nil {
		return "", err
	}
	result, err := self.browser.SendCDPCommand(ctx, "Target.createTarget", map[string]interface{}{
		"url": arguments.URL,
	}, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		TargetID string `json:"targetId"`
	}
	json.Unmarshal(result, &response)
	out, _ := json.Marshal(map[string]string{
		"targetId": response.TargetID,
		"url":      arguments.URL,
	})
	return string(out), nil
}

// --- browser_tab_close ---

type browserTabCloseTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"targetId": map[string]interface{}{"type": "string", "description": "Target ID of the closed tab"},
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

	sessionId, err := resolveSessionId(self.browser, "")
	if err != nil {
		return "", err
	}
	params := map[string]interface{}{}
	if arguments.TargetID != "" {
		params["targetId"] = arguments.TargetID
	}
	_, err = self.browser.SendCDPCommand(ctx, "Target.closeTarget", params, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"targetId": arguments.TargetID})
	return string(out), nil
}

// --- browser_tab_activate ---

type browserTabActivateTool struct{ browser Browser }

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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"targetId": map[string]interface{}{"type": "string", "description": "Target ID of the activated tab"},
				},
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

	sessionId, err := resolveSessionId(self.browser, "")
	if err != nil {
		return "", err
	}
	_, err = self.browser.SendCDPCommand(ctx, "Target.activateTarget", map[string]interface{}{
		"targetId": arguments.TargetID,
	}, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"targetId": arguments.TargetID})
	return string(out), nil
}
