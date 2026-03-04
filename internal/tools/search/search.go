package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/tools"
)

var log = logging.MustGetLogger("search")

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&searchTool{}}
	})
}

// braveApiKeyFromContext reads the Brave API key from the store configuration.
func braveApiKeyFromContext(ctx context.Context) string {
	var apiKey string
	dataStore := store.StoreFromContextSafe(ctx)
	if dataStore == nil {
		return apiKey
	}
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if configuration.Tools != nil {
			apiKey = configuration.Tools.GetBraveAPIKey()
		}
		return nil
	})
	return apiKey
}

type searchTool struct{}

func (self *searchTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
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
			Returns: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"results": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"title":       map[string]interface{}{"type": "string", "description": "Result title"},
								"url":         map[string]interface{}{"type": "string", "description": "Result URL"},
								"description": map[string]interface{}{"type": "string", "description": "Result description"},
							},
						},
					},
				},
			},
		},
	}
}

func (self *searchTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	apiKey := braveApiKeyFromContext(ctx)
	if apiKey == "" {
		apiKey = os.Getenv("BRAVE_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("Brave Search API key not configured")
	}

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

	log.Debugf("GET brave search query=%q count=%d", arguments.Query, arguments.Count)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, searchUrl, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Subscription-Token", apiKey)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("executing search: %w", err)
	}
	defer response.Body.Close()

	log.Debugf("GET brave search status=%d", response.StatusCode)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("search API returned %d: %s", response.StatusCode, string(body))
	}

	var apiResult struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &apiResult); err != nil {
		return "", fmt.Errorf("parsing search results: %w", err)
	}

	log.Debugf("brave search returned %d results for query=%q", len(apiResult.Web.Results), arguments.Query)

	type resultEntry struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	entries := make([]resultEntry, len(apiResult.Web.Results))
	for index, searchResult := range apiResult.Web.Results {
		entries[index] = resultEntry{
			Title:       searchResult.Title,
			URL:         searchResult.URL,
			Description: searchResult.Description,
		}
	}
	output, _ := json.Marshal(map[string]interface{}{"results": entries})
	return string(output), nil
}
