// Package fetch exposes a tool for fetching remote content.
package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/version"
)

var log = logging.MustGetLogger("web_fetch")

const maxFetchBodyBytes = 128 * 1024 // 128 KB

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&fetchTool{}}
	})
}

type fetchTool struct{}

func (self *fetchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "web_fetch",
			Description: "Fetch content from any URL via HTTP GET. Returns the response body as text. Useful for reading web pages, APIs, RSS feeds, or any publicly accessible URL.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch content from.",
					},
					"headers": map[string]interface{}{
						"type":        "object",
						"description": "Optional HTTP headers to include in the request.",
					},
				},
				"required": []string{"url"},
			},
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type":        "integer",
						"description": "HTTP status code.",
					},
					"contentType": map[string]interface{}{
						"type":        "string",
						"description": "Content-Type header from the response.",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Response body text.",
					},
					"truncated": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the response body was truncated.",
					},
				},
			},
		},
	}
}

func (self *fetchTool) Policy(ctx context.Context, arguments string) tools.PolicyDecision {
	return tools.AllowPolicy()
}

func (self *fetchTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.URL == "" {
		return "", fmt.Errorf("url is required")
	}

	// Validate the URL scheme.
	if !strings.HasPrefix(arguments.URL, "http://") && !strings.HasPrefix(arguments.URL, "https://") {
		return "", fmt.Errorf("url must start with http:// or https://")
	}

	log.Debugf("GET %s", arguments.URL)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, arguments.URL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	request.Header.Set("User-Agent", version.ServerName())
	for key, value := range arguments.Headers {
		request.Header.Set(key, value)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetching url: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	log.Debugf("GET %s status=%d", arguments.URL, response.StatusCode)

	limitedReader := io.LimitReader(response.Body, maxFetchBodyBytes+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}

	truncated := len(body) > maxFetchBodyBytes
	if truncated {
		body = body[:maxFetchBodyBytes]
	}

	result, err := json.Marshal(map[string]interface{}{
		"status":      response.StatusCode,
		"contentType": response.Header.Get("Content-Type"),
		"body":        string(body),
		"truncated":   truncated,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}
