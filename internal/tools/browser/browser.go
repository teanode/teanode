// Package browser exposes tools for browser-driven interactions.
package browser

import (
	"context"
	"encoding/json"
	"fmt"

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
				"snapshot (get accessibility tree with stable [ref=N] markers on interactive elements), " +
				"click (click by selector or coordinates), click_ref (click element by snapshot ref), " +
				"type (type text into focused/selected element), type_ref (type into element by ref), " +
				"hover_ref (hover over element by ref), select_option (select dropdown option by ref), " +
				"press_key (press keyboard key), evaluate (run JavaScript), " +
				"wait (wait for condition: selector, navigation, network_idle, timeout), " +
				"execute_script (run multiple browser actions in sequence), " +
				"intercept_start (start capturing network requests), intercept_stop (stop and return captured requests), " +
				"get_intercepted (return captured requests without stopping).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{
							"navigate", "screenshot", "snapshot",
							"click", "click_ref", "type", "type_ref",
							"hover_ref", "select_option",
							"press_key", "evaluate",
							"wait", "execute_script",
							"intercept_start", "intercept_stop", "get_intercepted",
						},
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
					"ref": map[string]interface{}{
						"type":        "integer",
						"description": "Element ref number from the last snapshot (for click_ref, type_ref, hover_ref, select_option).",
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
						"description": "CSS selector of the element (for click, type, wait actions).",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "The text to type (for type, type_ref actions).",
					},
					"clearFirst": map[string]interface{}{
						"type":        "boolean",
						"description": "Clear the input field before typing (for type_ref). Defaults to false.",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "The key to press, e.g. \"Enter\", \"Tab\", \"Escape\", \"ArrowDown\" (for press_key action).",
					},
					"expression": map[string]interface{}{
						"type":        "string",
						"description": "JavaScript expression to evaluate (for evaluate action).",
					},
					"waitMode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"selector", "navigation", "network_idle", "timeout"},
						"description": "What to wait for (for wait action). 'selector' waits for a CSS selector, 'navigation' for page load, 'network_idle' for no pending requests, 'timeout' for a fixed duration.",
					},
					"timeoutMs": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in milliseconds (for wait action). Default 30000.",
					},
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "Array of action steps to execute in sequence (for execute_script). Each step is an object with the same fields as a regular browser action (action, ref, selector, text, etc.). Max 50 steps.",
						"items": map[string]interface{}{
							"type": "object",
						},
					},
					"optionValue": map[string]interface{}{
						"type":        "string",
						"description": "Value of the option to select (for select_option action).",
					},
					"optionIndex": map[string]interface{}{
						"type":        "integer",
						"description": "Zero-based index of the option to select (for select_option action).",
					},
					"urlPattern": map[string]interface{}{
						"type":        "string",
						"description": "Regex pattern to filter captured URLs (for intercept_start). Empty captures all requests.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"description": "Action-dependent result. snapshot: {tree, refCount, pageUrl, title} with [ref=N] markers. " +
					"click_ref/type_ref/hover_ref: {ref, role, name, ...}. wait: {mode, elapsed}. " +
					"execute_script: {stepsExecuted, totalSteps, results}. intercept_*: {requests, count}.",
			},
		},
	}
}

func (self *browserTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"screenshot", "snapshot", "get_intercepted"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *browserTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	browser := browsers.BrowserFromContext(ctx)
	if browser == nil {
		return "", fmt.Errorf("no browser available")
	}

	var arguments struct {
		Action       string       `json:"action"`
		ConnectionID string       `json:"connectionId"`
		URL          string       `json:"url"`
		Ref          *int         `json:"ref"`
		X            *float64     `json:"x"`
		Y            *float64     `json:"y"`
		Selector     string       `json:"selector"`
		Text         string       `json:"text"`
		ClearFirst   bool         `json:"clearFirst"`
		Key          string       `json:"key"`
		Expression   string       `json:"expression"`
		WaitMode     string       `json:"waitMode"`
		TimeoutMs    *int         `json:"timeoutMs"`
		Steps        []scriptStep `json:"steps"`
		OptionValue  string       `json:"optionValue"`
		OptionIndex  *int         `json:"optionIndex"`
		URLPattern   string       `json:"urlPattern"`
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
		return executeEnhancedSnapshot(ctx, browser, arguments.ConnectionID)
	case "click":
		return executeClick(ctx, browser, arguments.ConnectionID, arguments.X, arguments.Y, arguments.Selector)
	case "click_ref":
		if arguments.Ref == nil {
			return "", fmt.Errorf("ref is required for click_ref action")
		}
		return executeClickRef(ctx, browser, arguments.ConnectionID, *arguments.Ref)
	case "type":
		return executeType(ctx, browser, arguments.ConnectionID, arguments.Text, arguments.Selector)
	case "type_ref":
		if arguments.Ref == nil {
			return "", fmt.Errorf("ref is required for type_ref action")
		}
		return executeTypeRef(ctx, browser, arguments.ConnectionID, *arguments.Ref, arguments.Text, arguments.ClearFirst)
	case "hover_ref":
		if arguments.Ref == nil {
			return "", fmt.Errorf("ref is required for hover_ref action")
		}
		return executeHoverRef(ctx, browser, arguments.ConnectionID, *arguments.Ref)
	case "select_option":
		if arguments.Ref == nil {
			return "", fmt.Errorf("ref is required for select_option action")
		}
		return executeSelectOption(ctx, browser, arguments.ConnectionID, *arguments.Ref, arguments.OptionValue, arguments.OptionIndex)
	case "press_key":
		return executePressKey(ctx, browser, arguments.ConnectionID, arguments.Key)
	case "evaluate":
		return executeEvaluate(ctx, browser, arguments.ConnectionID, arguments.Expression)
	case "wait":
		return executeWait(ctx, browser, arguments.ConnectionID, arguments.WaitMode, arguments.Selector, arguments.TimeoutMs)
	case "execute_script":
		return executeScript(ctx, browser, arguments.ConnectionID, arguments.Steps)
	case "intercept_start":
		return executeInterceptStart(ctx, browser, arguments.ConnectionID, arguments.URLPattern)
	case "intercept_stop":
		return executeInterceptStop(ctx, browser, arguments.ConnectionID)
	case "get_intercepted":
		return executeGetIntercepted(ctx, browser, arguments.ConnectionID)
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

// executeSnapshot is the legacy snapshot handler (kept for reference).
// The main "snapshot" action now routes to executeEnhancedSnapshot in
// snapshot.go which adds [ref=N] markers to interactive elements.

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
			Description: "Manage browser tabs and named instances. Actions: list (list all tabs with names), " +
				"open (open new tab, optionally with a name), close (close a tab), activate (bring tab to foreground), " +
				"name (assign a name to an existing tab for easy reference), resolve (get connectionId by name).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list", "open", "close", "activate", "name", "resolve"},
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
					"name": map[string]interface{}{
						"type": "string",
						"description": "Human-readable name for the browser instance (for open, name, resolve actions). " +
							"Use meaningful names like 'login-page' or 'dashboard' so you can reference tabs by name later.",
					},
					"connectionId": map[string]interface{}{
						"type":        "string",
						"description": "Connection ID of the tab to name (for name action).",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"description": "Action-dependent result. list: {tabs: [{targetId, title, url, connectionId, source, name}]}. " +
					"open: {targetId, url, connectionId, name}. close: {targetId}. activate: {targetId}. " +
					"name: {name, connectionId}. resolve: {name, connectionId}.",
			},
		},
	}
}

func (self *browserTabsTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupRead, Default: models.ToolPolicyAnyone, Actions: []string{"list"}},
		{Group: models.ToolPolicyGroupWrite, Default: models.ToolPolicyAnyone},
	}
}

func (self *browserTabsTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	browser := browsers.BrowserFromContext(ctx)
	if browser == nil {
		return "", fmt.Errorf("no browser available")
	}

	var arguments struct {
		Action       string `json:"action"`
		TargetID     string `json:"targetId"`
		URL          string `json:"url"`
		Name         string `json:"name"`
		ConnectionID string `json:"connectionId"`
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
		return executeTabsOpen(ctx, browser, arguments.URL, arguments.Name)
	case "close":
		return executeTabsClose(ctx, browser, arguments.TargetID)
	case "activate":
		return executeTabsActivate(ctx, browser, arguments.TargetID)
	case "name":
		return executeTabsName(ctx, arguments.Name, arguments.ConnectionID)
	case "resolve":
		return executeTabsResolve(ctx, arguments.Name)
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
	namedInstances := globalInstanceStore.listForUser(userId)

	// Build a reverse map: connectionId → name.
	connectionIdToName := make(map[string]string)
	for name, connectionId := range namedInstances {
		connectionIdToName[connectionId] = name
	}

	type entry struct {
		TargetID     string `json:"targetId"`
		Title        string `json:"title"`
		URL          string `json:"url"`
		ConnectionID string `json:"connectionId"`
		Source       string `json:"source"`
		Name         string `json:"name,omitempty"`
	}
	entries := make([]entry, len(targets))
	for index, target := range targets {
		entries[index] = entry{
			TargetID:     target.TargetID,
			Title:        target.Title,
			URL:          target.URL,
			ConnectionID: target.SessionID,
			Source:       target.Source,
			Name:         connectionIdToName[target.SessionID],
		}
	}
	output, _ := json.Marshal(map[string]interface{}{"tabs": entries})
	return string(output), nil
}

func executeTabsOpen(ctx context.Context, browser browsers.Browser, url string, name string) (string, error) {
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

	// If a name was provided, assign it to the new tab's connection.
	// We need to find the connectionId for this targetId.
	connectionId := ""
	if user != nil && user.ID != "" {
		if scopedBrowser, ok := browser.(browsers.UserScopedBrowser); ok {
			for _, target := range scopedBrowser.TargetsForUser(user.ID) {
				if target.TargetID == response.TargetID {
					connectionId = target.SessionID
					break
				}
			}
		}
	}
	if name != "" && connectionId != "" && user != nil {
		globalInstanceStore.assign(user.ID, name, connectionId)
	}

	outputMap := map[string]string{
		"targetId": response.TargetID,
		"url":      url,
	}
	if connectionId != "" {
		outputMap["connectionId"] = connectionId
	}
	if name != "" {
		outputMap["name"] = name
	}
	output, _ := json.Marshal(outputMap)
	return string(output), nil
}

func executeTabsName(ctx context.Context, name string, connectionId string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for name action")
	}
	if connectionId == "" {
		return "", fmt.Errorf("connectionId is required for name action")
	}
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	globalInstanceStore.assign(user.ID, name, connectionId)
	output, _ := json.Marshal(map[string]string{
		"name":         name,
		"connectionId": connectionId,
	})
	return string(output), nil
}

func executeTabsResolve(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("name is required for resolve action")
	}
	user := models.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return "", fmt.Errorf("missing user context")
	}
	connectionId, err := globalInstanceStore.resolve(user.ID, name)
	if err != nil {
		return "", err
	}
	output, _ := json.Marshal(map[string]string{
		"name":         name,
		"connectionId": connectionId,
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

// --- Accessibility tree types ---
//
// The shared types (accessibilityValue, accessibilityProperty) and the
// ref-enhanced tree builder live in snapshot.go. The old non-ref
// accessibilityNode type was replaced by accessibilityNodeExt which adds
// backendDOMNodeId for ref-based interactions.

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
