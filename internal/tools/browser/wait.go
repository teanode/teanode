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
//   - navigation: wait for a new navigation or URL change, or for an already in-flight navigation to finish
//   - network_idle: wait until tracked page fetch/XMLHttpRequest activity has been idle for 500ms
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
		return "", fmt.Errorf("browser: unknown wait mode: %s (supported: selector, navigation, network_idle, timeout)", mode)
	}
}

type navigationWaitState struct {
	URL             string  `json:"url"`
	ReadyState      string  `json:"readyState"`
	TimeOrigin      float64 `json:"timeOrigin"`
	NavigationCount int     `json:"navigationCount"`
}

type networkIdleState struct {
	ActiveRequests  int     `json:"activeRequests"`
	LastActivityAt  float64 `json:"lastActivityAt"`
	CurrentTime     float64 `json:"currentTime"`
	ReadyState      string  `json:"readyState"`
	IdleThresholdMS int     `json:"idleThresholdMs"`
}

func waitForSelector(ctx context.Context, browser browsers.Browser, sessionId string, selector string) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("browser: selector is required for wait mode 'selector'")
	}

	expression := fmt.Sprintf(`document.querySelector(%q) !== null`, selector)
	startTime := time.Now()

	for {
		result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
			"expression":    expression,
			"returnByValue": true,
		}, sessionId)
		if err != nil {
			return "", fmt.Errorf("browser: wait selector: %w", err)
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
			return "", fmt.Errorf("browser: wait for selector %q timed out", selector)
		case <-time.After(pollInterval):
		}
	}
}

func waitForNavigation(ctx context.Context, browser browsers.Browser, sessionId string) (string, error) {
	startTime := time.Now()
	initialGeneration, _ := globalNavigationStore.snapshot(sessionId)
	initialState, err := readNavigationWaitState(ctx, browser, sessionId)
	if err != nil {
		return "", fmt.Errorf("browser: wait navigation: %w", err)
	}

	sawNavigation := initialState.ReadyState != "complete"

	for {
		currentState, err := readNavigationWaitState(ctx, browser, sessionId)
		if err != nil {
			return "", fmt.Errorf("browser: wait navigation: %w", err)
		}
		currentGeneration, lastKnownUrl := globalNavigationStore.snapshot(sessionId)

		if !sawNavigation {
			sawNavigation =
				currentGeneration != initialGeneration ||
					(lastKnownUrl != "" && lastKnownUrl != initialState.URL) ||
					currentState.URL != initialState.URL ||
					currentState.TimeOrigin != initialState.TimeOrigin ||
					currentState.NavigationCount != initialState.NavigationCount ||
					currentState.ReadyState != initialState.ReadyState
		}

		if sawNavigation && currentState.ReadyState == "complete" {
			output, _ := json.Marshal(map[string]interface{}{
				"mode":               "navigation",
				"readyState":         currentState.ReadyState,
				"url":                currentState.URL,
				"elapsed":            time.Since(startTime).Milliseconds(),
				"navigationDetected": true,
			})
			return string(output), nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("browser: wait for navigation timed out")
		case <-time.After(pollInterval):
		}
	}
}

func waitForNetworkIdle(ctx context.Context, browser browsers.Browser, sessionId string) (string, error) {
	startTime := time.Now()

	if _, err := ensureNetworkIdleTracker(ctx, browser, sessionId); err != nil {
		return "", fmt.Errorf("browser: wait network idle: %w", err)
	}

	for {
		state, err := ensureNetworkIdleTracker(ctx, browser, sessionId)
		if err != nil {
			return "", fmt.Errorf("browser: wait network idle: %w", err)
		}

		idleForMilliseconds := state.CurrentTime - state.LastActivityAt
		if state.ActiveRequests == 0 && idleForMilliseconds >= float64(state.IdleThresholdMS) {
			output, _ := json.Marshal(map[string]interface{}{
				"mode":            "network_idle",
				"elapsed":         time.Since(startTime).Milliseconds(),
				"activeRequests":  state.ActiveRequests,
				"idleForMs":       int64(idleForMilliseconds),
				"idleThresholdMs": state.IdleThresholdMS,
				"tracker":         "fetch_xhr",
				"readyState":      state.ReadyState,
			})
			return string(output), nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("browser: wait for network idle timed out")
		case <-time.After(pollInterval):
		}
	}
}

func readNavigationWaitState(ctx context.Context, browser browsers.Browser, sessionId string) (*navigationWaitState, error) {
	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    navigationWaitStateScript,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result struct {
			Value navigationWaitState `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}
	return &response.Result.Value, nil
}

func ensureNetworkIdleTracker(ctx context.Context, browser browsers.Browser, sessionId string) (*networkIdleState, error) {
	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    networkIdleTrackerScript,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return nil, err
	}

	var response struct {
		Result struct {
			Value networkIdleState `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}
	if response.Result.Value.IdleThresholdMS == 0 {
		response.Result.Value.IdleThresholdMS = 500
	}
	return &response.Result.Value, nil
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
