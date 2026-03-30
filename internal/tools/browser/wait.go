package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teanode/teanode/internal/integrations/browsers"
)

const (
	defaultWaitTimeout = 30 * time.Second
	pollInterval       = 200 * time.Millisecond
)

// executeWait waits for a browser condition to be met. Supported modes:
//   - selector: wait until a CSS selector matches at least one element
//   - navigation: wait for the page to finish navigating (load event)
//   - network_idle: wait until there are no pending network requests for 500ms
//   - timeout: simply wait for a specified duration (in milliseconds)
func executeWait(ctx context.Context, browser browsers.Browser, connectionId string, mode string, selector string, timeoutMs *int) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	timeout := defaultWaitTimeout
	if timeoutMs != nil && *timeoutMs > 0 {
		timeout = time.Duration(*timeoutMs) * time.Millisecond
	}

	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch mode {
	case "selector":
		return waitForSelector(waitContext, browser, sessionId, selector)
	case "navigation":
		return waitForNavigation(waitContext, browser, sessionId)
	case "network_idle":
		return waitForNetworkIdle(waitContext, browser, sessionId)
	case "timeout":
		return waitForTimeout(waitContext, timeout)
	default:
		return "", fmt.Errorf("unknown wait mode: %s (supported: selector, navigation, network_idle, timeout)", mode)
	}
}

func waitForSelector(ctx context.Context, browser browsers.Browser, sessionId string, selector string) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("selector is required for wait mode 'selector'")
	}

	expression := fmt.Sprintf(`document.querySelector(%q) !== null`, selector)
	startTime := time.Now()

	for {
		result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expression,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", fmt.Errorf("wait selector: %w", err)
		}

		var response struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(result, &response) == nil && response.Result.Value {
			output, _ := json.Marshal(map[string]interface{}{
				"mode":     "selector",
				"selector": selector,
				"found":    true,
				"elapsed":  time.Since(startTime).Milliseconds(),
			})
			return string(output), nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for selector %q timed out", selector)
		case <-time.After(pollInterval):
		}
	}
}

func waitForNavigation(ctx context.Context, browser browsers.Browser, sessionId string) (string, error) {
	// Enable Page events if not already enabled, then wait for loadEventFired.
	// Since we can't easily listen for CDP events in this architecture, we poll
	// document.readyState instead.
	startTime := time.Now()

	for {
		result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    `document.readyState`,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", fmt.Errorf("wait navigation: %w", err)
		}

		var response struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(result, &response) == nil && response.Result.Value == "complete" {
			output, _ := json.Marshal(map[string]interface{}{
				"mode":       "navigation",
				"readyState": "complete",
				"elapsed":    time.Since(startTime).Milliseconds(),
			})
			return string(output), nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for navigation timed out")
		case <-time.After(pollInterval):
		}
	}
}

func waitForNetworkIdle(ctx context.Context, browser browsers.Browser, sessionId string) (string, error) {
	// We use a JavaScript-based approach: inject a PerformanceObserver that
	// tracks pending resources. Since we can't hold persistent state across
	// CDP calls easily, we use a simpler heuristic: poll performance.getEntriesByType
	// and wait until no new entries appear for 500ms.
	startTime := time.Now()
	idleThreshold := 500 * time.Millisecond

	var lastEntryCount int
	var lastChangeTime time.Time

	for {
		result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    `performance.getEntriesByType('resource').length`,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", fmt.Errorf("wait network idle: %w", err)
		}

		var response struct {
			Result struct {
				Value float64 `json:"value"`
			} `json:"result"`
		}
		_ = json.Unmarshal(result, &response)
		currentCount := int(response.Result.Value)

		if currentCount != lastEntryCount {
			lastEntryCount = currentCount
			lastChangeTime = time.Now()
		}

		if !lastChangeTime.IsZero() && time.Since(lastChangeTime) >= idleThreshold {
			output, _ := json.Marshal(map[string]interface{}{
				"mode":          "network_idle",
				"resourceCount": currentCount,
				"elapsed":       time.Since(startTime).Milliseconds(),
			})
			return string(output), nil
		}

		if lastChangeTime.IsZero() {
			lastChangeTime = time.Now()
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for network idle timed out")
		case <-time.After(pollInterval):
		}
	}
}

func waitForTimeout(ctx context.Context, duration time.Duration) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(duration):
		output, _ := json.Marshal(map[string]interface{}{
			"mode":    "timeout",
			"elapsed": duration.Milliseconds(),
		})
		return string(output), nil
	}
}
