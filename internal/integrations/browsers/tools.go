package browsers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/provider"
)

// RegisterBrowserTools adds all browser-control tools to the registry.
func RegisterBrowserTools(registry *agents.ToolRegistry, browser Browser) {
	registry.Register(&browserTool{browser: browser})
	registry.Register(&browserTabsTool{browser: browser})
}

// resolveSessionId returns the sessionId for the given connectionId,
// or falls back to the default target's session ID.
func resolveSessionId(browser Browser, connectionId string) (string, error) {
	if connectionId != "" {
		target, err := browser.TargetByConnectionID(connectionId)
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

// --- browser (page interaction) ---

type browserTool struct{ browser Browser }

func (self *browserTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "browser",
			Description: "Interact with the browser page. Actions: navigate (go to URL), screenshot (capture page image), " +
				"snapshot (get accessibility tree), click (click element), type (type text), press_key (press keyboard key), " +
				"evaluate (run JavaScript).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"navigate", "screenshot", "snapshot", "click", "type", "press_key", "evaluate"},
						"description": "The browser action to perform.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Connection ID of the browser tab to target. If omitted, uses the default tab.",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to navigate to (for navigate action).",
					},
					"x": map[string]interface{}{
						"type":        "number",
						"description": "X coordinate to click (for click action).",
					},
					"y": map[string]interface{}{
						"type":        "number",
						"description": "Y coordinate to click (for click action).",
					},
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element (for click, type actions).",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to type (for type action).",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to press, e.g. \"Enter\", \"Tab\", \"Escape\", \"ArrowDown\" (for press_key action).",
					},
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript expression to evaluate (for evaluate action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent result. navigate: {url}. screenshot: {base64, format}. snapshot: {tree}. click: {selector} or {x, y}. type: {text}. press_key: {key}. evaluate: {type, value}.",
			},
		},
	}
}

func (self *browserTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action       string   `json:"action"`
		ConnectionID string   `json:"connectionId"`
		URL          string   `json:"url"`
		X            *float64 `json:"x"`
		Y            *float64 `json:"y"`
		Selector     string   `json:"selector"`
		Text         string   `json:"text"`
		Key          string   `json:"key"`
		Expression   string   `json:"expression"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "navigate":
		return self.executeNavigate(ctx, arguments.ConnectionID, arguments.URL)
	case "screenshot":
		return self.executeScreenshot(ctx, arguments.ConnectionID)
	case "snapshot":
		return self.executeSnapshot(ctx, arguments.ConnectionID)
	case "click":
		return self.executeClick(ctx, arguments.ConnectionID, arguments.X, arguments.Y, arguments.Selector)
	case "type":
		return self.executeType(ctx, arguments.ConnectionID, arguments.Text, arguments.Selector)
	case "press_key":
		return self.executePressKey(ctx, arguments.ConnectionID, arguments.Key)
	case "evaluate":
		return self.executeEvaluate(ctx, arguments.ConnectionID, arguments.Expression)
	default:
		return "", fmt.Errorf("unknown browser action: %s", arguments.Action)
	}
}

func (self *browserTool) executeNavigate(ctx context.Context, connectionId string, url string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
	if err != nil {
		return "", err
	}
	_, err = self.browser.SendCDPCommand(ctx, "Page.navigate", map[string]interface{}{
		"url": url,
	}, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"url": url})
	return string(out), nil
}

func (self *browserTool) executeScreenshot(ctx context.Context, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
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

func (self *browserTool) executeSnapshot(ctx context.Context, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
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

func (self *browserTool) executeClick(ctx context.Context, connectionId string, x *float64, y *float64, selector string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
	if err != nil {
		return "", err
	}

	if selector != "" {
		expression := fmt.Sprintf(`document.querySelector(%q)?.click()`, selector)
		_, err := self.browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expression,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
		out, _ := json.Marshal(map[string]string{"selector": selector})
		return string(out), nil
	}

	if x == nil || y == nil {
		return "", fmt.Errorf("provide either (x, y) coordinates or a selector")
	}
	xValue, yValue := *x, *y
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		_, err := self.browser.SendCDPCommand(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
			"type":       eventType,
			"x":          xValue,
			"y":          yValue,
			"button":     "left",
			"clickCount": 1,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}
	out, _ := json.Marshal(map[string]interface{}{"x": xValue, "y": yValue})
	return string(out), nil
}

func (self *browserTool) executeType(ctx context.Context, connectionId string, text string, selector string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
	if err != nil {
		return "", err
	}

	if selector != "" {
		expression := fmt.Sprintf(`document.querySelector(%q)?.focus()`, selector)
		_, err := self.browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expression,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}

	_, err = self.browser.SendCDPCommand(ctx, "Input.insertText", map[string]interface{}{
		"text": text,
	}, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"text": text})
	return string(out), nil
}

func (self *browserTool) executePressKey(ctx context.Context, connectionId string, key string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
	if err != nil {
		return "", err
	}

	info := keyInfo(key)

	for _, eventType := range []string{"keyDown", "keyUp"} {
		params := map[string]interface{}{
			"type": eventType,
			"key":  info.key,
		}
		if info.code != "" {
			params["code"] = info.code
		}
		if info.keyCode != 0 {
			params["windowsVirtualKeyCode"] = info.keyCode
			params["nativeVirtualKeyCode"] = info.keyCode
		}
		if eventType == "keyDown" && len(info.text) > 0 {
			params["text"] = info.text
		}
		_, err := self.browser.SendCDPCommand(ctx, "Input.dispatchKeyEvent", params, sessionId)
		if err != nil {
			return "", err
		}
	}
	out, _ := json.Marshal(map[string]string{"key": key})
	return string(out), nil
}

func (self *browserTool) executeEvaluate(ctx context.Context, connectionId string, expression string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, connectionId)
	if err != nil {
		return "", err
	}
	result, err := self.browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
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

// --- browser_tabs (tab management) ---

type browserTabsTool struct{ browser Browser }

func (self *browserTabsTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionSpec{
			Name: "browser_tabs",
			Description: "Manage browser tabs. Actions: list (list all tabs), open (open new tab), " +
				"close (close a tab), activate (bring tab to foreground).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "open", "close", "activate"},
						"description": "The tab management action to perform.",
					},
					"targetId": map[string]interface{}{
						"type":        "string",
						"description": "Target ID of the tab (for close, activate actions).",
					},
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to open in the new tab, defaults to about:blank (for open action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent result. list: {tabs: [{targetId, title, url, connectionId, source}]}. open: {targetId, url}. close: {targetId}. activate: {targetId}.",
			},
		},
	}
}

func (self *browserTabsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action   string `json:"action"`
		TargetID string `json:"targetId"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list":
		return self.executeList()
	case "open":
		return self.executeOpen(ctx, arguments.URL)
	case "close":
		return self.executeClose(ctx, arguments.TargetID)
	case "activate":
		return self.executeActivate(ctx, arguments.TargetID)
	default:
		return "", fmt.Errorf("unknown browser_tabs action: %s", arguments.Action)
	}
}

func (self *browserTabsTool) executeList() (string, error) {
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
	out, _ := json.Marshal(map[string]interface{}{"tabs": entries})
	return string(out), nil
}

func (self *browserTabsTool) executeOpen(ctx context.Context, url string) (string, error) {
	if url == "" {
		url = "about:blank"
	}

	sessionId, err := resolveSessionId(self.browser, "")
	if err != nil {
		return "", err
	}
	result, err := self.browser.SendCDPCommand(ctx, "Target.createTarget", map[string]interface{}{
		"url": url,
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
		"url":      url,
	})
	return string(out), nil
}

func (self *browserTabsTool) executeClose(ctx context.Context, targetId string) (string, error) {
	sessionId, err := resolveSessionId(self.browser, "")
	if err != nil {
		return "", err
	}
	params := map[string]interface{}{}
	if targetId != "" {
		params["targetId"] = targetId
	}
	_, err = self.browser.SendCDPCommand(ctx, "Target.closeTarget", params, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"targetId": targetId})
	return string(out), nil
}

func (self *browserTabsTool) executeActivate(ctx context.Context, targetId string) (string, error) {
	if targetId == "" {
		return "", fmt.Errorf("targetId is required for activate action")
	}

	sessionId, err := resolveSessionId(self.browser, "")
	if err != nil {
		return "", err
	}
	_, err = self.browser.SendCDPCommand(ctx, "Target.activateTarget", map[string]interface{}{
		"targetId": targetId,
	}, sessionId)
	if err != nil {
		return "", err
	}
	out, _ := json.Marshal(map[string]string{"targetId": targetId})
	return string(out), nil
}

// --- Accessibility tree types and helpers ---

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

	nodesById := make(map[string]*accessibilityNode, len(nodes))
	for index := range nodes {
		nodesById[nodes[index].NodeID] = &nodes[index]
	}

	var builder strings.Builder
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		node, ok := nodesById[id]
		if !ok || node.Ignored {
			return
		}

		role := fmt.Sprintf("%v", node.Role.Value)
		name := fmt.Sprintf("%v", node.Name.Value)

		// Skip generic/none roles without meaningful names.
		if (role == "none" || role == "generic" || role == "") && name == "" {
			for _, childId := range node.ChildIDs {
				walk(childId, depth)
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

		for _, childId := range node.ChildIDs {
			walk(childId, depth+1)
		}
	}

	// Start from the root (first node).
	walk(nodes[0].NodeID, 0)
	return strings.TrimRight(builder.String(), "\n")
}

// --- Key mapping helpers ---

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
