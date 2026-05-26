package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeoutSeconds = 10
	maxResponseBytes      = 2 * 1024 * 1024 // 2 MB
)

// Client abstracts HTTP communication with the Home Assistant REST API.
// Using an interface enables test mocking without real HA calls.
type Client interface {
	// GetStates returns all entity states (GET /api/states).
	GetStates(ctx context.Context) ([]EntityState, error)

	// GetState returns the state of a single entity (GET /api/states/{entity_id}).
	GetState(ctx context.Context, entityId string) (*EntityState, error)

	// CallService invokes a HA service (POST /api/services/{domain}/{service}).
	CallService(ctx context.Context, domain string, service string, data map[string]interface{}) (json.RawMessage, error)

	// GetHistory returns history for an entity (GET /api/history/period/{start}).
	GetHistory(ctx context.Context, entityId string, hours int) (json.RawMessage, error)

	// GetConfig returns the HA instance configuration (GET /api/config).
	GetConfig(ctx context.Context) (json.RawMessage, error)
}

// EntityState represents a Home Assistant entity state object.
type EntityState struct {
	EntityID    string                 `json:"entity_id"`
	State       string                 `json:"state"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	LastChanged string                 `json:"last_changed,omitempty"`
	LastUpdated string                 `json:"last_updated,omitempty"`
}

// httpClient implements Client using the Home Assistant REST API over HTTP.
type httpClient struct {
	baseUrl    string
	token      string
	httpClient *http.Client
}

// NewHTTPClient creates a new HTTP-based Home Assistant client.
func NewHTTPClient(baseUrl string, token string, timeoutSeconds int) Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultTimeoutSeconds
	}
	return &httpClient{
		baseUrl: strings.TrimRight(baseUrl, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
	}
}

func (self *httpClient) doRequest(ctx context.Context, method string, path string, body io.Reader) ([]byte, error) {
	url := self.baseUrl + path

	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("homeassistant: creating request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+self.token)
	request.Header.Set("Content-Type", "application/json")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("homeassistant: request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("homeassistant: reading response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Do not leak raw HA error bodies — extract a safe message.
		return nil, fmt.Errorf("homeassistant: home assistant returned HTTP %d for %s %s", response.StatusCode, method, path)
	}

	return responseBody, nil
}

func (self *httpClient) GetStates(ctx context.Context) ([]EntityState, error) {
	data, err := self.doRequest(ctx, http.MethodGet, "/api/states", nil)
	if err != nil {
		return nil, err
	}
	var states []EntityState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("homeassistant: parsing states response: %w", err)
	}
	return states, nil
}

func (self *httpClient) GetState(ctx context.Context, entityId string) (*EntityState, error) {
	data, err := self.doRequest(ctx, http.MethodGet, "/api/states/"+entityId, nil)
	if err != nil {
		return nil, err
	}
	var state EntityState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("homeassistant: parsing state response: %w", err)
	}
	return &state, nil
}

func (self *httpClient) CallService(ctx context.Context, domain string, service string, data map[string]interface{}) (json.RawMessage, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("homeassistant: marshaling service data: %w", err)
	}

	path := fmt.Sprintf("/api/services/%s/%s", domain, service)
	responseData, err := self.doRequest(ctx, http.MethodPost, path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(responseData), nil
}

func (self *httpClient) GetHistory(ctx context.Context, entityId string, hours int) (json.RawMessage, error) {
	if hours <= 0 {
		hours = 1
	}
	start := time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339)
	path := fmt.Sprintf("/api/history/period/%s?filter_entity_id=%s&minimal_response", start, entityId)

	data, err := self.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func (self *httpClient) GetConfig(ctx context.Context) (json.RawMessage, error) {
	data, err := self.doRequest(ctx, http.MethodGet, "/api/config", nil)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
