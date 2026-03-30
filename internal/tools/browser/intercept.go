package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/teanode/teanode/internal/integrations/browsers"
)

// interceptedRequest holds a captured network request/response.
type interceptedRequest struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Status     int               `json:"status,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	MIMEType   string            `json:"mimeType,omitempty"`
	BodyLength int               `json:"bodyLength,omitempty"`
}

// interceptStore holds captured requests per session.
type interceptStore struct {
	sessions map[string][]interceptedRequest
	mutex    sync.Mutex
}

var globalInterceptStore = &interceptStore{
	sessions: make(map[string][]interceptedRequest),
}

func (self *interceptStore) clear(sessionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.sessions, sessionId)
}

// executeInterceptStart enables network interception using the CDP Network
// domain. It enables Network.enable and sets up request logging via
// a JavaScript-injected approach using Performance API for basic capture.
//
// Note: Full Fetch-domain interception requires persistent event listening
// which is not supported in the current CDP architecture (request→response).
// Instead, we use Network.enable + a polling approach.
func executeInterceptStart(ctx context.Context, browser browsers.Browser, connectionId string, urlPattern string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	// Enable the Network domain for this session.
	_, err = browser.SendCDPCommand(ctx, "Network.enable", map[string]interface{}{
		"maxTotalBufferSize":    10 * 1024 * 1024,
		"maxResourceBufferSize": 5 * 1024 * 1024,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("enabling network interception: %w", err)
	}

	// Inject a PerformanceObserver to track resource loads with a URL filter.
	script := fmt.Sprintf(`(() => {
		if (window.__teanodeNetIntercept) {
			window.__teanodeNetIntercept.disconnect();
		}
		window.__teanodeNetCaptures = [];
		const pattern = %q;
		const regex = pattern ? new RegExp(pattern) : null;
		window.__teanodeNetIntercept = new PerformanceObserver((list) => {
			for (const entry of list.getEntries()) {
				if (entry.entryType === 'resource') {
					if (!regex || regex.test(entry.name)) {
						window.__teanodeNetCaptures.push({
							url: entry.name,
							method: entry.initiatorType,
							duration: Math.round(entry.duration),
							transferSize: entry.transferSize || 0,
							status: entry.responseStatus || 0,
						});
					}
				}
			}
		});
		window.__teanodeNetIntercept.observe({type: 'resource', buffered: false});
		return true;
	})()`, urlPattern)

	_, err = browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    script,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return "", fmt.Errorf("injecting network observer: %w", err)
	}

	// Clear any previously captured entries.
	globalInterceptStore.clear(sessionId)

	output, _ := json.Marshal(map[string]interface{}{
		"status":     "active",
		"urlPattern": urlPattern,
	})
	return string(output), nil
}

// executeInterceptStop disables network interception and returns all captured requests.
func executeInterceptStop(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	// Harvest captured entries from the injected observer.
	entries, err := harvestInterceptedEntries(ctx, browser, sessionId)
	if err != nil {
		return "", err
	}

	// Clean up the observer.
	_, _ = browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": `(() => {
			if (window.__teanodeNetIntercept) {
				window.__teanodeNetIntercept.disconnect();
				delete window.__teanodeNetIntercept;
			}
			const captures = window.__teanodeNetCaptures || [];
			delete window.__teanodeNetCaptures;
			return captures.length;
		})()`,
		"returnByValue": true,
	}, sessionId)

	// Disable network domain.
	_, _ = browser.SendCDPCommand(ctx, "Network.disable", nil, sessionId)

	output, _ := json.Marshal(map[string]interface{}{
		"status":   "stopped",
		"requests": entries,
		"count":    len(entries),
	})
	return string(output), nil
}

// executeGetIntercepted returns currently captured network requests without
// stopping the interception.
func executeGetIntercepted(ctx context.Context, browser browsers.Browser, connectionId string) (string, error) {
	sessionId, err := resolveSessionId(ctx, browser, connectionId)
	if err != nil {
		return "", err
	}

	entries, err := harvestInterceptedEntries(ctx, browser, sessionId)
	if err != nil {
		return "", err
	}

	output, _ := json.Marshal(map[string]interface{}{
		"requests": entries,
		"count":    len(entries),
	})
	return string(output), nil
}

// harvestInterceptedEntries retrieves captured entries from the page's
// injected PerformanceObserver.
func harvestInterceptedEntries(ctx context.Context, browser browsers.Browser, sessionId string) ([]interceptedRequest, error) {
	result, err := browser.SendCDPCommand(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression": `(() => {
			const captures = window.__teanodeNetCaptures || [];
			window.__teanodeNetCaptures = [];
			return JSON.stringify(captures);
		})()`,
		"returnByValue": true,
	}, sessionId)
	if err != nil {
		return nil, fmt.Errorf("harvesting intercepted requests: %w", err)
	}

	var evalResponse struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &evalResponse); err != nil {
		return nil, err
	}

	// The value is a JSON string that needs double-unmarshal.
	var raw string
	var entries []interceptedRequest
	if json.Unmarshal(evalResponse.Result.Value, &raw) == nil {
		_ = json.Unmarshal([]byte(raw), &entries)
	} else {
		_ = json.Unmarshal(evalResponse.Result.Value, &entries)
	}

	if entries == nil {
		entries = []interceptedRequest{}
	}
	return entries, nil
}
