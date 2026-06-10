package unifiprotect

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeoutSeconds = 15
	maxResponseBytes      = 4 * 1024 * 1024  // 4 MB for JSON responses
	maxSnapshotBytes      = 10 * 1024 * 1024 // 10 MB for snapshot images
)

// Camera represents a UniFi Protect camera.
type Camera struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	State           string `json:"state"`
	IsConnected     bool   `json:"isConnected"`
	IsDoorbell      bool   `json:"isDoorbell"`
	Host            string `json:"host"`
	MAC             string `json:"mac"`
	FirmwareVersion string `json:"firmwareVersion"`
	StatusLight     bool   `json:"isDark"`
	RecordingMode   string `json:"recordingMode"`
	IsPrivacyOn     bool   `json:"isPrivacyOn"`
}

// bootstrapResponse is the relevant subset of the Protect private bootstrap API response.
type bootstrapResponse struct {
	Cameras []cameraRaw `json:"cameras"`
}

// cameraRaw represents the raw camera JSON from the private bootstrap API.
type cameraRaw struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`
	State             string                 `json:"state"`
	IsConnected       bool                   `json:"isConnected"`
	Host              string                 `json:"host"`
	MAC               string                 `json:"mac"`
	FeatureFlags      map[string]interface{} `json:"featureFlags"`
	RecordingSettings map[string]interface{} `json:"recordingSettings"`
	IsDark            bool                   `json:"isDark"`
	PrivacyZones      []interface{}          `json:"privacyZones"`
	FirmwareVersion   string                 `json:"firmwareVersion"`
}

// toCamera converts a raw bootstrap camera to the clean Camera type.
func (self *cameraRaw) toCamera() Camera {
	camera := Camera{
		ID:              self.ID,
		Name:            self.Name,
		Type:            self.Type,
		State:           self.State,
		IsConnected:     self.IsConnected,
		Host:            self.Host,
		MAC:             self.MAC,
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

// integrationCamera represents a camera from the official integration API v1.
type integrationCamera struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	State       string `json:"state"`
	IsConnected bool   `json:"isConnected"`
	Host        string `json:"host"`
	MAC         string `json:"mac"`
	IsDoorbell  bool   `json:"isDoorbell"`
	// The integration API may omit some fields available in the private API.
}

// toCamera converts an integration API camera to the clean Camera type.
func (self *integrationCamera) toCamera() Camera {
	return Camera{
		ID:          self.ID,
		Name:        self.Name,
		Type:        self.Type,
		State:       self.State,
		IsConnected: self.IsConnected,
		Host:        self.Host,
		MAC:         self.MAC,
		IsDoorbell:  self.IsDoorbell,
	}
}

// Client abstracts HTTP communication with the UniFi Protect API.
type Client interface {
	// GetCameras returns all cameras.
	GetCameras(ctx context.Context) ([]Camera, error)

	// GetSnapshot fetches a JPEG snapshot for a camera by ID.
	GetSnapshot(ctx context.Context, cameraId string) ([]byte, error)

	// PatchCamera updates camera settings via PATCH (private API only).
	PatchCamera(ctx context.Context, cameraId string, payload map[string]interface{}) error
}

// httpClient implements Client using the UniFi Protect API.
// When apiKey is set, it uses the official integration API v1 with X-API-KEY header.
// When username/password is set, it uses cookie-based auth with the private API.
type httpClient struct {
	baseUrl    string
	apiKey     string
	username   string
	password   string
	httpClient *http.Client

	// Cookie-based auth state (username/password mode only).
	authMutex     sync.Mutex
	authCookie    string
	csrfToken     string
	authenticated bool
}

// NewHTTPClient creates a new HTTP-based UniFi Protect client.
func NewHTTPClient(config *resolvedConfiguration) Client {
	timeoutSeconds := config.timeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultTimeoutSeconds
	}

	transport := &http.Transport{}
	if !config.verifyTls {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &httpClient{
		baseUrl:  strings.TrimRight(config.baseUrl, "/"),
		apiKey:   config.apiKey,
		username: config.username,
		password: config.password,
		httpClient: &http.Client{
			Timeout:   time.Duration(timeoutSeconds) * time.Second,
			Transport: transport,
			// Do not follow redirects automatically so we can capture auth cookies.
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// useIntegrationApi returns true when API key auth is configured, meaning
// the official integration API v1 endpoints should be used.
func (self *httpClient) useIntegrationApi() bool {
	return self.apiKey != ""
}

// login performs cookie-based authentication against the UniFi OS console.
// POST /api/auth/login with username/password, then stores the auth cookie
// and CSRF token for subsequent requests.
func (self *httpClient) login(ctx context.Context) error {
	loginPayload, err := json.Marshal(map[string]interface{}{
		"username":   self.username,
		"password":   self.password,
		"rememberMe": true,
	})
	if err != nil {
		return fmt.Errorf("unifiprotect: marshaling login payload: %w", err)
	}

	loginUrl := self.baseUrl + "/api/auth/login"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, loginUrl, bytes.NewReader(loginPayload))
	if err != nil {
		return fmt.Errorf("unifiprotect: creating login request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("unifiprotect: login request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	// Drain the response body.
	_, _ = io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("unifiprotect: login failed with HTTP %d (check username/password)", response.StatusCode)
	}

	// Extract the TOKEN cookie.
	for _, cookie := range response.Cookies() {
		if cookie.Name == "TOKEN" {
			self.authCookie = cookie.Value
			break
		}
	}
	if self.authCookie == "" {
		return fmt.Errorf("unifiprotect: login succeeded but no TOKEN cookie returned")
	}

	// Extract CSRF token from response headers.
	csrfToken := response.Header.Get("X-Updated-Csrf-Token")
	if csrfToken == "" {
		csrfToken = response.Header.Get("X-Csrf-Token")
	}
	if csrfToken != "" {
		self.csrfToken = csrfToken
	}

	self.authenticated = true
	return nil
}

// ensureAuthenticated performs login if using cookie-based auth and not yet authenticated.
func (self *httpClient) ensureAuthenticated(ctx context.Context) error {
	if self.useIntegrationApi() {
		return nil // API key auth doesn't need login.
	}

	self.authMutex.Lock()
	defer self.authMutex.Unlock()

	if self.authenticated {
		return nil
	}

	return self.login(ctx)
}

// doRequest executes an HTTP request with the appropriate authentication.
func (self *httpClient) doRequest(ctx context.Context, method string, path string, body io.Reader, maxBytes int64) ([]byte, int, error) {
	requestUrl := self.baseUrl + path

	request, err := http.NewRequestWithContext(ctx, method, requestUrl, body)
	if err != nil {
		return nil, 0, fmt.Errorf("unifiprotect: creating request: %w", err)
	}

	if self.useIntegrationApi() {
		request.Header.Set("X-API-KEY", self.apiKey)
	} else {
		// Cookie-based auth.
		self.authMutex.Lock()
		cookie := self.authCookie
		csrfToken := self.csrfToken
		self.authMutex.Unlock()

		if cookie != "" {
			request.Header.Set("Cookie", "TOKEN="+cookie)
		}
		if csrfToken != "" {
			request.Header.Set("X-CSRF-Token", csrfToken)
		}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("unifiprotect: request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxBytes))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("unifiprotect: reading response: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("unifiprotect: UniFi Protect returned HTTP %d for %s %s", response.StatusCode, method, path)
	}

	return responseBody, response.StatusCode, nil
}

// doAuthenticatedRequest performs a request with cookie auth, retrying login once on 401.
func (self *httpClient) doAuthenticatedRequest(ctx context.Context, method string, path string, body io.Reader, maxBytes int64) ([]byte, error) {
	if err := self.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	data, statusCode, err := self.doRequest(ctx, method, path, body, maxBytes)
	if err != nil && statusCode == http.StatusUnauthorized && !self.useIntegrationApi() {
		// Session expired — re-login and retry once.
		self.authMutex.Lock()
		self.authenticated = false
		self.authMutex.Unlock()

		if loginErr := self.ensureAuthenticated(ctx); loginErr != nil {
			return nil, fmt.Errorf("unifiprotect: re-authentication failed: %w", loginErr)
		}

		data, _, err = self.doRequest(ctx, method, path, body, maxBytes)
	}
	return data, err
}

func (self *httpClient) GetCameras(ctx context.Context) ([]Camera, error) {
	if self.useIntegrationApi() {
		return self.getCamerasIntegrationApi(ctx)
	}
	return self.getCamerasPrivateApi(ctx)
}

// getCamerasIntegrationApi lists cameras via the official integration API v1.
func (self *httpClient) getCamerasIntegrationApi(ctx context.Context) ([]Camera, error) {
	data, _, err := self.doRequest(ctx, http.MethodGet, "/proxy/protect/integration/v1/cameras", nil, maxResponseBytes)
	if err != nil {
		return nil, err
	}

	var rawCameras []integrationCamera
	if err := json.Unmarshal(data, &rawCameras); err != nil {
		return nil, fmt.Errorf("unifiprotect: parsing integration cameras response: %w", err)
	}

	cameras := make([]Camera, len(rawCameras))
	for index, raw := range rawCameras {
		cameras[index] = raw.toCamera()
	}
	return cameras, nil
}

// getCamerasPrivateApi lists cameras via the private bootstrap API.
func (self *httpClient) getCamerasPrivateApi(ctx context.Context) ([]Camera, error) {
	data, err := self.doAuthenticatedRequest(ctx, http.MethodGet, "/proxy/protect/api/bootstrap", nil, maxResponseBytes)
	if err != nil {
		return nil, err
	}

	var bootstrap bootstrapResponse
	if err := json.Unmarshal(data, &bootstrap); err != nil {
		return nil, fmt.Errorf("unifiprotect: parsing bootstrap response: %w", err)
	}

	cameras := make([]Camera, len(bootstrap.Cameras))
	for index, raw := range bootstrap.Cameras {
		cameras[index] = raw.toCamera()
	}
	return cameras, nil
}

func (self *httpClient) GetSnapshot(ctx context.Context, cameraId string) ([]byte, error) {
	if self.useIntegrationApi() {
		return self.getSnapshotIntegrationApi(ctx, cameraId)
	}
	return self.getSnapshotPrivateApi(ctx, cameraId)
}

// getSnapshotIntegrationApi fetches a snapshot via the official integration API v1.
func (self *httpClient) getSnapshotIntegrationApi(ctx context.Context, cameraId string) ([]byte, error) {
	path := fmt.Sprintf("/proxy/protect/integration/v1/cameras/%s/snapshot", url.PathEscape(cameraId))
	requestUrl := self.baseUrl + path

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("unifiprotect: creating snapshot request: %w", err)
	}

	request.Header.Set("X-API-KEY", self.apiKey)
	request.Header.Set("Accept", "image/jpeg")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("unifiprotect: snapshot request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("unifiprotect: UniFi Protect returned HTTP %d for snapshot request", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSnapshotBytes))
	if err != nil {
		return nil, fmt.Errorf("unifiprotect: reading snapshot: %w", err)
	}

	return data, nil
}

// getSnapshotPrivateApi fetches a snapshot via the private API with cookie auth.
func (self *httpClient) getSnapshotPrivateApi(ctx context.Context, cameraId string) ([]byte, error) {
	if err := self.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/proxy/protect/api/cameras/%s/snapshot?force=true", url.PathEscape(cameraId))
	requestUrl := self.baseUrl + path

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("unifiprotect: creating snapshot request: %w", err)
	}

	self.authMutex.Lock()
	cookie := self.authCookie
	csrfToken := self.csrfToken
	self.authMutex.Unlock()

	if cookie != "" {
		request.Header.Set("Cookie", "TOKEN="+cookie)
	}
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	request.Header.Set("Accept", "image/jpeg")

	response, err := self.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("unifiprotect: snapshot request failed: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("unifiprotect: UniFi Protect returned HTTP %d for snapshot request", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSnapshotBytes))
	if err != nil {
		return nil, fmt.Errorf("unifiprotect: reading snapshot: %w", err)
	}

	return data, nil
}

func (self *httpClient) PatchCamera(ctx context.Context, cameraId string, payload map[string]interface{}) error {
	if self.useIntegrationApi() {
		return fmt.Errorf("unifiprotect: camera settings modification requires username/password authentication; the official integration API (apiKey) does not support PATCH operations")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("unifiprotect: marshaling patch payload: %w", err)
	}

	path := fmt.Sprintf("/proxy/protect/api/cameras/%s", url.PathEscape(cameraId))
	_, err = self.doAuthenticatedRequest(ctx, http.MethodPatch, path, bytes.NewReader(payloadBytes), maxResponseBytes)
	return err
}
