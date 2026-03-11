package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/integrations/browsers"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&browserTool{}, &browserTabsTool{}}
	})
}

// resolveSessionId returns the sessionId for the given user and connectionId,
// or falls back to the user's default target's session ID.
func resolveSessionId(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	scopedBrowser, ok := browser.(browsers.UserScopedBrowser)
	if !ok {
		return "", fmt.Errorf("browser backend does not support user scoping")
	}
	if connectionId != "" {
		target, err := scopedBrowser.TargetByConnectionIDForUser(user.ID, connectionId)
		if err != nil {
			return "", err
		}
		return target.SessionID, nil
	}
	target, err := scopedBrowser.DefaultTargetForUser(user.ID)
	if err != nil {
		return "", err
	}
	return target.SessionID, nil
}

// --- browser (page interaction) ---

type browserTool struct{}

func (self *browserTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
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
	browser := browsers.BrowserFromContext(ctx)
	if browser == nil {
		return "", fmt.Errorf("no browser available")
	}

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
		return executeNavigate(ctx, browser, arguments.ConnectionID, arguments.URL)
	case "screenshot":
		return executeScreenshot(ctx, browser, arguments.ConnectionID)
	case "snapshot":
		return executeSnapshot(ctx, browser, arguments.ConnectionID)
	case "click":
		return executeClick(ctx, browser, arguments.ConnectionID, arguments.X, arguments.Y, arguments.Selector)
	case "type":
		return executeType(ctx, browser, arguments.ConnectionID, arguments.Text, arguments.Selector)
	case "press_key":
		return executePressKey(ctx, browser, arguments.ConnectionID, arguments.Key)
	case "evaluate":
		return executeEvaluate(ctx, browser, arguments.ConnectionID, arguments.Expression)
	default:
		return "", fmt.Errorf("unknown browser action: %s", arguments.Action)
	}
}

func executeNavigate(ctx context.Context, browser browsers.Browser, connectionId string, url string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}
	_, err = browser.SendCDPCommand(ctx, "Page.navigate", map[string]interface{}{
		"url": url,
	}, sessionId)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"url": url})
	return string(output), nil
}

func executeScreenshot(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}
	result, err := browser.SendCDPCommand(ctx, "Page.captureScreenshot", map[string]interface{}{
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
	output, _ := json.Marshal(map[string]string{
		"base64": response.Data,
		"format": "png",
	})
	return string(output), nil
}

func executeSnapshot(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}
	result, err := browser.SendCDPCommand(ctx, "Accessibility.getFullAXTree", nil, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		Nodes []accessibilityNode `json:"nodes"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("parsing accessibility tree: %w", err)
	}
	output, _ := json.Marshal(map[string]string{"tree": buildAXTree(response.Nodes)})
	return string(output), nil
}

func executeClick(ctx context.Context, browser browsers.Browser, connectionId string, x *float64, y *float64, selector string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	if selector != "" {
		expression := fmt.Sprintf(`document.querySelector(%q)?.click()`, selector)
		_, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expression,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
		output, _ := json.Marshal(map[string]string{"selector": selector})
		return string(output), nil
	}

	if x == nil || y == nil {
		return "", fmt.Errorf("provide either (x, y) coordinates or a selector")
	}
	xValue, yValue := *x, *y
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		_, err := browser.SendCDPCommand(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
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
	output, _ := json.Marshal(map[string]interface{}{"x": xValue, "y": yValue})
	return string(output), nil
}

func executeType(ctx context.Context, browser browsers.Browser, connectionId string, text string, selector string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	if selector != "" {
		expression := fmt.Sprintf(`document.querySelector(%q)?.focus()`, selector)
		_, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expression,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", err
		}
	}

	_, err = browser.SendCDPCommand(ctx, "Input.insertText", map[string]interface{}{
		"text": text,
	}, sessionId)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"text": text})
	return string(output), nil
}

func executePressKey(ctx context.Context, browser browsers.Browser, connectionId string, key string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	information := keyInformation(key)

	for _, eventType := range []string{"keyDown", "keyUp"} {
		parameters := map[string]interface{}{
			"type": eventType,
			"key":  information.key,
		}
		if information.code != "" {
			parameters["code"] = information.code
		}
		if information.keyCode != 0 {
			parameters["windowsVirtualKeyCode"] = information.keyCode
			parameters["nativeVirtualKeyCode"] = information.keyCode
		}
		if eventType == "keyDown" && len(information.text) > 0 {
			parameters["text"] = information.text
		}
		_, err := browser.SendCDPCommand(ctx, "Input.dispatchKeyEvent", parameters, sessionId)
		if err != nil {
			return "", err
		}
	}
	output, _ := json.Marshal(map[string]string{"key": key})
	return string(output), nil
}

func executeEvaluate(ctx context.Context, browser browsers.Browser, connectionId string, expression string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}
	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
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
	output, _ := json.Marshal(map[string]interface{}{
		"type":  response.Result.Type,
		"value": response.Result.Value,
	})
	return string(output), nil
}

// --- browser_tabs (tab management) ---

type browserTabsTool struct{}

func (self *browserTabsTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
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
	browser := browsers.BrowserFromContext(ctx)
	if browser == nil {
		return "", fmt.Errorf("no browser available")
	}

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
		user := models.UserFromContext(ctx)
		if user == nil || user.ID == "" {
			return "", fmt.Errorf("missing user context")
		}
		return executeTabsList(browser, user.ID)
	case "open":
		return executeTabsOpen(ctx, browser, arguments.URL)
	case "close":
		return executeTabsClose(ctx, browser, arguments.TargetID)
	case "activate":
		return executeTabsActivate(ctx, browser, arguments.TargetID)
	default:
		return "", fmt.Errorf("unknown browser_tabs action: %s", arguments.Action)
	}
}

func executeTabsList(browser browsers.Browser, userId string) (string, error) {
	scopedBrowser, ok := browser.(browsers.UserScopedBrowser)
	if !ok {
		return "", fmt.Errorf("browser backend does not support user scoping")
	}
	targets := scopedBrowser.TargetsForUser(userId)
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
	output, _ := json.Marshal(map[string]interface{}{"tabs": entries})
	return string(output), nil
}

func executeTabsOpen(ctx context.Context, browser browsers.Browser, url string) (string, error) {
	if url == "" {
		url = "about:blank"
	}

	sessionId, err := resolveSessionId(ctx, browser, "")
	if err != nil {
		return "", err
	}
	result, err := browser.SendCDPCommand(ctx, "Target.createTarget", map[string]interface{}{
		"url": url,
	}, sessionId)
	if err != nil {
		return "", err
	}
	var response struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("unmarshal createTarget response: %w", err)
	}
	user := models.UserFromContext(ctx)
	if assigner, ok := browser.(browsers.TargetOwnerAssigner); ok && user != nil && user.ID != "" && response.TargetID != "" {
		assigner.AssignTargetToUser(user.ID, response.TargetID)
	}
	output, _ := json.Marshal(map[string]string{
		"targetId": response.TargetID,
		"url":      url,
	})
	return string(output), nil
}

func executeTabsClose(ctx context.Context, browser browsers.Browser, targetId string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	scopedBrowser, ok := browser.(browsers.UserScopedBrowser)
	if !ok {
		return "", fmt.Errorf("browser backend does not support user scoping")
	}
	if targetId == "" {
		target, err := scopedBrowser.DefaultTargetForUser(user.ID)
		if err != nil {
			return "", err
		}
		targetId = target.TargetID
	}
	allowed := false
	for _, target := range scopedBrowser.TargetsForUser(user.ID) {
		if target.TargetID == targetId {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("targetId %q not found", targetId)
	}
	sessionId, err := resolveSessionId(ctx, browser, "")
	if err != nil {
		return "", err
	}
	parameters := map[string]interface{}{}
	if targetId != "" {
		parameters["targetId"] = targetId
	}
	_, err = browser.SendCDPCommand(ctx, "Target.closeTarget", parameters, sessionId)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"targetId": targetId})
	return string(output), nil
}

func executeTabsActivate(ctx context.Context, browser browsers.Browser, targetId string) (string, error) {
	if targetId == "" {
		return "", fmt.Errorf("targetId is required for activate action")
	}
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	scopedBrowser, ok := browser.(browsers.UserScopedBrowser)
	if !ok {
		return "", fmt.Errorf("browser backend does not support user scoping")
	}
	allowed := false
	for _, target := range scopedBrowser.TargetsForUser(user.ID) {
		if target.TargetID == targetId {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("targetId %q not found", targetId)
	}

	sessionId, err := resolveSessionId(ctx, browser, "")
	if err != nil {
		return "", err
	}
	_, err = browser.SendCDPCommand(ctx, "Target.activateTarget", map[string]interface{}{
		"targetId": targetId,
	}, sessionId)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{"targetId": targetId})
	return string(output), nil
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

func keyInformation(key string) keyData {
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
