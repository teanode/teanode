package unifiprotect

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
)

const (
	defaultTimeoutSeconds = 15
	maxResponseBytes      = 4 * 1024 * 1024  // 4 MB for JSON responses
	maxSnapshotBytes      = 10 * 1024 * 1024 // 10 MB for snapshot images
)

// Camera represents a UniFi Protect camera from the bootstrap data.
type Camera struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	State           string `json:"state"`
	IsConnected     bool   `json:"isConnected"`
	IsDoorbell      bool   `json:"featureFlags.isDoorbell"`
	Host            string `json:"host"`
	Mac             string `json:"mac"`
	FirmwareVersion string `json:"firmwareVersion"`
	StatusLight     bool   `json:"isDark"`
	RecordingMode   string `json:"recordingSettings.mode"`
	IsPrivacyOn     bool   `json:"privacyZones"`
}

// bootstrapResponse is the relevant subset of the Protect bootstrap API response.
type bootstrapResponse struct {
	Cameras []cameraRaw `json:"cameras"`
}

// cameraRaw represents the raw camera JSON from the bootstrap API.
type cameraRaw struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	State       string                 `json:"state"`
	IsConnected bool                   `json:"isConnected"`
	Host        string                 `json:"host"`
	Mac         string                 `json:"mac"`
	FeatureFlags map[string]interface{} `json:"featureFlags"`
	RecordingSettings map[string]interface{} `json:"recordingSettings"`
	IsDark      bool                   `json:"isDark"`
	PrivacyZones []interface{}         `json:"privacyZones"`
	FirmwareVersion string             `json:"firmwareVersion"`
}

// toCamera converts a raw camera response to the clean Camera type.
func (self *cameraRaw) toCamera() Camera {
	camera := Camera{
		ID:              self.ID,
		Name:            self.Name,
		Type:            self.Type,
		State:           self.State,
		IsConnected:     self.IsConnected,
		Host:            self.Host,
		Mac:             self.Mac,
		FirmwareVersion: self.FirmwareVersion,
		StatusLight:     self.IsDark,
		IsPrivacyOn:     len(self.PrivacyZones) > 0,
	}

	if self.FeatureFlags != nil {
		if isDoorbell, ok := self.FeatureFlags["isDoorbell"].(bool); ok {
			camera.IsDoorbell = isDoorbell
		}
	}
	if self.RecordingSettings != nil {
		if mode, ok := self.RecordingSettings["mode"].(string); ok {
			camera.RecordingMode = mode
		}
	}

	return camera
}

// Client abstracts HTTP communication with the UniFi Protect API.
type Client interface {
	// GetCameras returns all cameras from the bootstrap endpoint.
	GetCameras(ctx context.Context) ([]Camera, error)

	// GetSnapshot fetches a JPEG snapshot for a camera by ID.
	GetSnapshot(ctx context.Context, cameraID string) ([]byte, error)

	// PatchCamera updates camera settings via PATCH.
	PatchCamera(ctx context.Context, cameraID string, payload map[string]interface{}) error
}

// httpClient implements Client using the UniFi Protect local API.
type httpClient struct {
	baseURL    string
	apiKey     string
	username   string
	password   string
	httpClient *http.Client
}

// NewHTTPClient creates a new HTTP-based UniFi Protect client.
func NewHTTPClient(config *configs.UniFiProtectConfig) Client {
	timeoutSeconds := config.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultTimeoutSeconds
	}

	transport := &http.Transport{}
	if !config.VerifyTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &httpClient{
		baseURL:  strings.TrimRight(config.BaseURL, "/"),
		apiKey:   config.APIKey,
		username: config.Username,
		password: config.Password,
		httpClient: &http.Client{
			Timeout:   time.Duration(timeoutSeconds) * time.Second,
			Transport: transport,
		},
	}
}

func (self *httpClient) doRequest(ctx context.Context, method string, path string, body io.Reader, maxBytes int64) ([]byte, error) {
	url := self.baseURL + path

	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Prefer API key auth; fall back to basic auth.
	if self.apiKey != "" {
		request.Header.Set("X-API-Key", self.apiKey)
	} else if self.username != "" && self.password != "" {
		request.SetBasicAuth(self.username, self.password)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("UniFi Protect returned HTTP %d for %s %s", response.StatusCode, method, path)
	}

	return responseBody, nil
}

func (self *httpClient) GetCameras(ctx context.Context) ([]Camera, error) {
	data, err := self.doRequest(ctx, http.MethodGet, "/proxy/protect/api/bootstrap", nil, maxResponseBytes)
	if err != nil {
		return nil, err
	}

	var bootstrap bootstrapResponse
	if err := json.Unmarshal(data, &bootstrap); err != nil {
		return nil, fmt.Errorf("parsing bootstrap response: %w", err)
	}

	cameras := make([]Camera, len(bootstrap.Cameras))
	for index, raw := range bootstrap.Cameras {
		cameras[index] = raw.toCamera()
	}
	return cameras, nil
}

func (self *httpClient) GetSnapshot(ctx context.Context, cameraID string) ([]byte, error) {
	path := fmt.Sprintf("/proxy/protect/api/cameras/%s/snapshot?force=true", cameraID)

	url := self.baseURL + path
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating snapshot request: %w", err)
	}

	if self.apiKey != "" {
		request.Header.Set("X-API-Key", self.apiKey)
	} else if self.username != "" && self.password != "" {
		request.SetBasicAuth(self.username, self.password)
	}
	request.Header.Set("Accept", "image/jpeg")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("snapshot request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("UniFi Protect returned HTTP %d for snapshot request", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSnapshotBytes))
	if err != nil {
		return nil, fmt.Errorf("reading snapshot: %w", err)
	}

	return data, nil
}

func (self *httpClient) PatchCamera(ctx context.Context, cameraID string, payload map[string]interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling patch payload: %w", err)
	}

	path := fmt.Sprintf("/proxy/protect/api/cameras/%s", cameraID)
	_, err = self.doRequest(ctx, http.MethodPatch, path, strings.NewReader(string(payloadBytes)), maxResponseBytes)
	return err
}
