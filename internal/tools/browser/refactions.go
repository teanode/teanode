package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/integrations/browsers"
)

// resolveReferenceToObjectId resolves a snapshot reference to a CDP remote object ID by
// accessing the DOM element stored in window.__teanodeRefs during the last
// snapshot. Returns the objectId for use with Runtime.callFunctionOn,
// DOM.getContentQuads, etc.
func resolveReferenceToObjectId(ctx context.Context, browser browsers.Browser, sessionId string, reference int) (string, error) {
	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": fmt.Sprintf("window.__teanodeRefs && window.__teanodeRefs[%d]", reference),
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: resolving reference %d: %w", reference, err)
	}

	var response struct {
		Result struct {
			Type     string `json:"type"`
			ObjectID string `json:"objectId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return "", fmt.Errorf("browser: parsing reference %d resolution: %w", reference, err)
	}
	if response.Result.ObjectID == "" || response.Result.Type == "undefined" {
		globalReferenceStore.clear(sessionId)
		return "", fmt.Errorf("browser: reference %d could not be resolved — the page may have navigated since the last snapshot", reference)
	}
	return response.Result.ObjectID, nil
}

// executeClickReference clicks an element identified by its snapshot reference number.
// It resolves the reference to a DOM node via window.__teanodeRefs, scrolls it into
// view, computes its center coordinates, and dispatches a click.
func executeClickReference(ctx context.Context, browser browsers.Browser, connectionId string, reference int) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	entry, err := globalReferenceStore.lookup(sessionId, reference)
	if err != nil {
		return "", err
	}

	objectId, err := resolveReferenceToObjectId(ctx, browser, sessionId, reference)
	if err != nil {
		return "", err
	}

	// Scroll into view and get clickable point.
	centerX, centerY, err := getClickablePoint(ctx, browser, sessionId, objectId)
	if err != nil {
		// Fallback: use JavaScript click.
		_, jsErr := browser.SendCDPCommand(ctx, "Runtime.callFunctionOn", map[string]interface{}{
			"objectId":            objectId,
			"functionDeclaration": `function() { this.scrollIntoView({block:"center"}); this.click(); }`,
			"returnByValue":       true,
		}, sessionId)
		if jsErr != nil {
			return "", fmt.Errorf("browser: clicking reference %d: %w", reference, jsErr)
		}
		output, _ := json.Marshal(map[string]interface{}{
			"reference": reference,
			"role":      entry.Role,
			"name":      entry.Name,
			"method":    "javascript",
		})
		return string(output), nil
	}

	// Dispatch mouse click at the center point.
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		_, err := browser.SendCDPCommand(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
			"type":       eventType,
			"x":          centerX,
			"y":          centerY,
			"button":     "left",
			"clickCount": 1,
		}, sessionId)
		if err != nil {
			return "", fmt.Errorf("browser: clicking reference %d: %w", reference, err)
		}
	}

	output, _ := json.Marshal(map[string]interface{}{
		"reference": reference,
		"role":      entry.Role,
		"name":      entry.Name,
		"x":         centerX,
		"y":         centerY,
		"method":    "coordinates",
	})
	return string(output), nil
}

// executeTypeReference focuses an element identified by reference and types text into it.
func executeTypeReference(ctx context.Context, browser browsers.Browser, connectionId string, reference int, text string, clearFirst bool) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	entry, err := globalReferenceStore.lookup(sessionId, reference)
	if err != nil {
		return "", err
	}

	objectId, err := resolveReferenceToObjectId(ctx, browser, sessionId, reference)
	if err != nil {
		return "", err
	}

	// Focus the element.
	_, err = browser.SendCDPCommand(ctx, "Runtime.callFunctionOn", map[string]interface{}{
		"objectId":            objectId,
		"functionDeclaration": `function() { this.scrollIntoView({block:"center"}); this.focus(); }`,
		"returnByValue":       true,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: focusing reference %d: %w", reference, err)
	}

	// Optionally clear the field first.
	if clearFirst {
		_, err = browser.SendCDPCommand(ctx, "Runtime.callFunctionOn", map[string]interface{}{
			"objectId":            objectId,
			"functionDeclaration": `function() { this.value = ''; this.dispatchEvent(new Event('input', {bubbles:true})); }`,
			"returnByValue":       true,
		}, sessionId)
		if err != nil {
			return "", fmt.Errorf("browser: clearing reference %d: %w", reference, err)
		}
	}

	// Type the text.
	_, err = browser.SendCDPCommand(ctx, "Input.insertText", map[string]interface{}{
		"text": text,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: typing into reference %d: %w", reference, err)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"reference":  reference,
		"role":       entry.Role,
		"name":       entry.Name,
		"text":       text,
		"clearFirst": clearFirst,
	})
	return string(output), nil
}

// executeHoverReference moves the mouse over an element identified by reference.
func executeHoverReference(ctx context.Context, browser browsers.Browser, connectionId string, reference int) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	entry, err := globalReferenceStore.lookup(sessionId, reference)
	if err != nil {
		return "", err
	}

	objectId, err := resolveReferenceToObjectId(ctx, browser, sessionId, reference)
	if err != nil {
		return "", err
	}

	centerX, centerY, err := getClickablePoint(ctx, browser, sessionId, objectId)
	if err != nil {
		return "", fmt.Errorf("browser: getting position for reference %d: %w", reference, err)
	}

	_, err = browser.SendCDPCommand(ctx, "Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mouseMoved",
		"x":    centerX,
		"y":    centerY,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: hovering reference %d: %w", reference, err)
	}

	output, _ := json.Marshal(map[string]interface{}{
		"reference": reference,
		"role":      entry.Role,
		"name":      entry.Name,
		"x":         centerX,
		"y":         centerY,
	})
	return string(output), nil
}

// executeSelectOption selects an <option> in a <select> element by reference.
// The optionValue or optionIndex parameter identifies which option to pick.
func executeSelectOption(ctx context.Context, browser browsers.Browser, connectionId string, reference int, optionValue string, optionIndex *int) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	entry, err := globalReferenceStore.lookup(sessionId, reference)
	if err != nil {
		return "", err
	}

	objectId, err := resolveReferenceToObjectId(ctx, browser, sessionId, reference)
	if err != nil {
		return "", err
	}

	// Build the selection script.
	var script string
	if optionIndex != nil {
		script = fmt.Sprintf(`function() {
			this.selectedIndex = %d;
			this.dispatchEvent(new Event('change', {bubbles: true}));
			return this.options[this.selectedIndex] ? this.options[this.selectedIndex].value : null;
		}`, *optionIndex)
	} else {
		script = fmt.Sprintf(`function() {
			this.value = %q;
			this.dispatchEvent(new Event('change', {bubbles: true}));
			return this.value;
		}`, optionValue)
	}

	selectResult, err := browser.SendCDPCommand(ctx, "Runtime.callFunctionOn", map[string]interface{}{
		"objectId":            objectId,
		"functionDeclaration": script,
		"returnByValue":       true,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: selecting option on reference %d: %w", reference, err)
	}

	var selectResponse struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
	}
	_ = json.Unmarshal(selectResult, &selectResponse)

	output, _ := json.Marshal(map[string]interface{}{
		"reference":     reference,
		"role":          entry.Role,
		"name":          entry.Name,
		"selectedValue": selectResponse.Result.Value,
	})
	return string(output), nil
}

// getClickablePoint scrolls an element into view and returns its center
// coordinates using DOM.getContentQuads.
func getClickablePoint(ctx context.Context, browser browsers.Browser, sessionId string, objectId string) (float64, float64, error) {
	// Scroll into view first.
	_, err := browser.SendCDPCommand(ctx, "Runtime.callFunctionOn", map[string]interface{}{
		"objectId":            objectId,
		"functionDeclaration": `function() { this.scrollIntoView({block:"center", inline:"center"}); }`,
		"returnByValue":       true,
	}, sessionId)
	if err != nil {
		return 0, 0, err
	}

	// Get content quads (more reliable than getBoxModel for visible area).
	quadsResult, err := browser.SendCDPCommand(ctx, "DOM.getContentQuads", map[string]interface{}{
		"objectId": objectId,
	}, sessionId)
	if err != nil {
		return 0, 0, err
	}

	var quadsResponse struct {
		Quads [][]float64 `json:"quads"`
	}
	if err := json.Unmarshal(quadsResult, &quadsResponse); err != nil || len(quadsResponse.Quads) == 0 {
		return 0, 0, fmt.Errorf("browser: no content quads available")
	}

	// Each quad is [x1,y1, x2,y2, x3,y3, x4,y4]. Compute the center.
	quad := quadsResponse.Quads[0]
	if len(quad) < 8 {
		return 0, 0, fmt.Errorf("browser: unexpected quad format")
	}
	centerX := (quad[0] + quad[2] + quad[4] + quad[6]) / 4
	centerY := (quad[1] + quad[3] + quad[5] + quad[7]) / 4

	return centerX, centerY, nil
}
