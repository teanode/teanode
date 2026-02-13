package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/logging"
	"github.com/ziyan/teanode/internal/provider"
)

var searchLog = logging.Get("search")

// RegisterSearchTools adds web search tools to the registry.
// If apiKey is empty, no tools are registered.
func RegisterSearchTools(registry *ToolRegistry, apiKey string) {
	if apiKey == "" {
		return
	}
	registry.Register(&webSearchTool{apiKey: apiKey})
}

type webSearchTool struct {
	apiKey string
}

func (self *webSearchTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        "web_search",
			Description: "Search the web using Brave Search and return results with titles, URLs, and descriptions.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query.",
					},
					"count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of results to return (default 5, max 20).",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func (self *webSearchTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Query string `json:"query"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}
	if arguments.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if arguments.Count <= 0 {
		arguments.Count = 5
	}
	if arguments.Count > 20 {
		arguments.Count = 20
	}

	searchUrl := "https://api.search.brave.com/res/v1/web/search?q=" +
		url.QueryEscape(arguments.Query) + "&count=" + strconv.Itoa(arguments.Count)

	searchLog.Debugf("GET brave search query=%q count=%d", arguments.Query, arguments.Count)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, searchUrl, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Subscription-Token", self.apiKey)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("executing search: %w", err)
	}
	defer response.Body.Close()

	searchLog.Debugf("GET brave search status=%d", response.StatusCode)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("search API returned %d: %s", response.StatusCode, string(body))
	}

	var result struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parsing search results: %w", err)
	}

	searchLog.Debugf("brave search returned %d results for query=%q", len(result.Web.Results), arguments.Query)

	if len(result.Web.Results) == 0 {
		return "no results found", nil
	}

	var builder strings.Builder
	for index, searchResult := range result.Web.Results {
		if index > 0 {
			builder.WriteString("\n\n")
		}
		fmt.Fprintf(&builder, "[%d] %s\n%s\n%s", index+1, searchResult.Title, searchResult.URL, searchResult.Description)
	}
	return builder.String(), nil
}
