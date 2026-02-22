package v1api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/media"
)

func withTempConfigDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })
	return directory
}

func newTestAPI(t *testing.T, cachedProfile *configs.Profile, mediaStore *media.Store) *v1Api {
	t.Helper()
	return New(
		gw.New(
			&configs.Config{},
			&configs.SecurityConfig{},
			cachedProfile,
			agents.NewAgentRegistry(),
			nil,
			nil,
			nil,
			nil,
			mediaStore,
			nil,
		),
		func() {},
	)
}

func decodeProfileResponse(t *testing.T, recorder *httptest.ResponseRecorder) configs.Profile {
	t.Helper()
	var profile configs.Profile
	if err := json.Unmarshal(recorder.Body.Bytes(), &profile); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return profile
}

func uploadAvatarRequest(t *testing.T) *http.Request {
	t.Helper()
	var imageBuffer bytes.Buffer
	imageData := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageBuffer, imageData); err != nil {
		t.Fatalf("failed to encode png: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("failed to create multipart file: %v", err)
	}
	if _, err := fileWriter.Write(imageBuffer.Bytes()); err != nil {
		t.Fatalf("failed to write multipart data: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/profile/avatar", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func newRPCWebSocketPair(t *testing.T, api *v1Api) (*webSocketConnection, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
	serverConnectionCh := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("failed to upgrade websocket: %v", err)
			return
		}
		serverConnectionCh <- connection
	}))
	webSocketUrl := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConnection, _, err := websocket.DefaultDialer.Dial(webSocketUrl, nil)
	if err != nil {
		server.Close()
		t.Fatalf("failed to dial websocket: %v", err)
	}
	serverConnection := <-serverConnectionCh
	connection := newWebSocketConnection(serverConnection, api, "test-session")
	cleanup := func() {
		_ = clientConnection.Close()
		_ = serverConnection.Close()
		server.Close()
	}
	return connection, clientConnection, cleanup
}

func readRPCResponse(t *testing.T, connection *websocket.Conn) responseFrame {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("failed to set read deadline: %v", err)
	}
	var response responseFrame
	if err := connection.ReadJSON(&response); err != nil {
		t.Fatalf("failed to read rpc response: %v", err)
	}
	return response
}

type rpcProfileResponsePayload struct {
	Name          string `json:"name"`
	Biography     string `json:"biography"`
	AvatarMediaID string `json:"avatarMediaId"`
}

func decodeRPCProfilePayload(t *testing.T, payload interface{}) rpcProfileResponsePayload {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	var decoded rpcProfileResponsePayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("failed to decode payload: %v", err)
	}
	return decoded
}

func TestHandleProfileGet_ReadsFromDiskWhenGatewayCacheIsStale(t *testing.T) {
	withTempConfigDirectory(t)
	persisted := &configs.Profile{
		Name:          "Disk Name",
		Bio:           "Disk Bio",
		AvatarMediaID: "disk_avatar",
	}
	if err := configs.SaveProfile(persisted); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	api := newTestAPI(t, &configs.Profile{
		Name:          "Stale Name",
		Bio:           "Stale Bio",
		AvatarMediaID: "stale_avatar",
	}, nil)

	response := httptest.NewRecorder()
	if err := api.handleProfile(response, httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)); err != nil {
		t.Fatalf("handleProfile GET failed: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}

	got := decodeProfileResponse(t, response)
	if got.Name != persisted.Name || got.Bio != persisted.Bio || got.AvatarMediaID != persisted.AvatarMediaID {
		t.Fatalf("response profile = %+v, want %+v", got, *persisted)
	}

	cached := api.gateway.Profile()
	if cached == nil {
		t.Fatal("gateway profile cache is nil")
	}
	if cached.Name != persisted.Name || cached.Bio != persisted.Bio || cached.AvatarMediaID != persisted.AvatarMediaID {
		t.Fatalf("gateway cache = %+v, want %+v", *cached, *persisted)
	}
}

func TestProfilePut_PersistsAndLoadsFromNewAPIInstance(t *testing.T) {
	withTempConfigDirectory(t)
	initial := &configs.Profile{
		Name:          "Before",
		Bio:           "Before Bio",
		AvatarMediaID: "avatar_before",
	}
	if err := configs.SaveProfile(initial); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	api := newTestAPI(t, &configs.Profile{
		Name:          "Stale",
		Bio:           "Stale Bio",
		AvatarMediaID: "stale_avatar",
	}, nil)

	putBody := strings.NewReader("{\"name\":\"  Updated Name  \",\"bio\":\"# Updated Bio\\n\\n- line one\"}")
	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/profile", putBody)
	response := httptest.NewRecorder()
	if err := api.handleProfile(response, putRequest); err != nil {
		t.Fatalf("handleProfile PUT failed: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	updated := decodeProfileResponse(t, response)
	if updated.Name != "Updated Name" || updated.Bio != "# Updated Bio\n\n- line one" {
		t.Fatalf("updated profile = %+v, want trimmed name and raw markdown bio", updated)
	}
	if updated.AvatarMediaID != initial.AvatarMediaID {
		t.Fatalf("avatarMediaId = %q, want %q", updated.AvatarMediaID, initial.AvatarMediaID)
	}

	refreshedAPI := newTestAPI(t, &configs.Profile{
		Name:          "Very Stale",
		Bio:           "Very Stale Bio",
		AvatarMediaID: "very_stale_avatar",
	}, nil)
	getResponse := httptest.NewRecorder()
	if err := refreshedAPI.handleProfile(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)); err != nil {
		t.Fatalf("handleProfile GET failed: %v", err)
	}
	if getResponse.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", getResponse.Code, http.StatusOK)
	}

	got := decodeProfileResponse(t, getResponse)
	if got.Name != "Updated Name" || got.Bio != "# Updated Bio\n\n- line one" || got.AvatarMediaID != initial.AvatarMediaID {
		t.Fatalf("profile after new api instance = %+v, want updated persisted values", got)
	}
}

func TestProfileAvatarUploadAndRemove_PersistAcrossRefresh(t *testing.T) {
	withTempConfigDirectory(t)
	if err := configs.SaveProfile(&configs.Profile{Name: "Alice"}); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	mediaStore := media.NewStore(t.TempDir())
	api := newTestAPI(t, &configs.Profile{
		Name:          "Stale Alice",
		Bio:           "Stale Bio",
		AvatarMediaID: "stale_avatar",
	}, mediaStore)

	uploadResponse := httptest.NewRecorder()
	if err := api.handleProfileAvatar(uploadResponse, uploadAvatarRequest(t)); err != nil {
		t.Fatalf("handleProfileAvatar POST failed: %v", err)
	}
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", uploadResponse.Code, http.StatusOK)
	}
	uploaded := decodeProfileResponse(t, uploadResponse)
	if uploaded.AvatarMediaID == "" {
		t.Fatal("avatarMediaId should not be empty after upload")
	}

	refreshedAPI := newTestAPI(t, &configs.Profile{
		Name:          "Very Stale Alice",
		Bio:           "Very Stale Bio",
		AvatarMediaID: "",
	}, mediaStore)
	getResponse := httptest.NewRecorder()
	if err := refreshedAPI.handleProfile(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)); err != nil {
		t.Fatalf("handleProfile GET failed: %v", err)
	}
	got := decodeProfileResponse(t, getResponse)
	if got.AvatarMediaID != uploaded.AvatarMediaID {
		t.Fatalf("avatarMediaId after refresh = %q, want %q", got.AvatarMediaID, uploaded.AvatarMediaID)
	}

	deleteResponse := httptest.NewRecorder()
	if err := refreshedAPI.handleProfileAvatar(deleteResponse, httptest.NewRequest(http.MethodDelete, "/api/v1/profile/avatar", nil)); err != nil {
		t.Fatalf("handleProfileAvatar DELETE failed: %v", err)
	}
	removed := decodeProfileResponse(t, deleteResponse)
	if removed.AvatarMediaID != "" {
		t.Fatalf("avatarMediaId after remove = %q, want empty", removed.AvatarMediaID)
	}

	afterRemoveAPI := newTestAPI(t, &configs.Profile{
		Name:          "Stale Again",
		Bio:           "Stale Again Bio",
		AvatarMediaID: uploaded.AvatarMediaID,
	}, mediaStore)
	finalGet := httptest.NewRecorder()
	if err := afterRemoveAPI.handleProfile(finalGet, httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)); err != nil {
		t.Fatalf("handleProfile GET failed: %v", err)
	}
	final := decodeProfileResponse(t, finalGet)
	if final.AvatarMediaID != "" {
		t.Fatalf("avatarMediaId after refresh post-remove = %q, want empty", final.AvatarMediaID)
	}
}

func TestWebSocketProfileRPCMethods(t *testing.T) {
	withTempConfigDirectory(t)
	initial := &configs.Profile{
		Name:          "Disk Name",
		Bio:           "Disk Bio",
		AvatarMediaID: "avatar_initial",
	}
	if err := configs.SaveProfile(initial); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	api := newTestAPI(t, &configs.Profile{
		Name:          "Stale Name",
		Bio:           "Stale Bio",
		AvatarMediaID: "stale_avatar",
	}, media.NewStore(t.TempDir()))
	connection, clientConnection, cleanup := newRPCWebSocketPair(t, api)
	defer cleanup()

	t.Run("profile.get", func(t *testing.T) {
		connection.dispatch(requestFrame{Type: "req", ID: "1", Method: "profile.get"})
		response := readRPCResponse(t, clientConnection)
		if !response.OK {
			t.Fatalf("response ok = false, error = %+v", response.Error)
		}
		payload := decodeRPCProfilePayload(t, response.Payload)
		if payload.Name != initial.Name || payload.Biography != initial.Bio || payload.AvatarMediaID != initial.AvatarMediaID {
			t.Fatalf("payload = %+v, want name/biography/avatar from persisted profile", payload)
		}
	})

	t.Run("profile.update", func(t *testing.T) {
		connection.dispatch(requestFrame{
			Type:   "req",
			ID:     "2",
			Method: "profile.update",
			Params: json.RawMessage("{\"name\":\"  Updated Name  \",\"biography\":\"# Updated Bio\\n\\n- line one\"}"),
		})
		response := readRPCResponse(t, clientConnection)
		if !response.OK {
			t.Fatalf("response ok = false, error = %+v", response.Error)
		}
		payload := decodeRPCProfilePayload(t, response.Payload)
		if payload.Name != "Updated Name" || payload.Biography != "# Updated Bio\n\n- line one" {
			t.Fatalf("payload = %+v, want trimmed name and raw markdown biography", payload)
		}
		if payload.AvatarMediaID != initial.AvatarMediaID {
			t.Fatalf("avatarMediaId = %q, want %q", payload.AvatarMediaID, initial.AvatarMediaID)
		}

		persisted, err := configs.LoadProfile()
		if err != nil {
			t.Fatalf("LoadProfile failed: %v", err)
		}
		if persisted.Name != "Updated Name" || persisted.Bio != "# Updated Bio\n\n- line one" || persisted.AvatarMediaID != initial.AvatarMediaID {
			t.Fatalf("persisted profile = %+v, want updated values with original avatar", *persisted)
		}
	})

	t.Run("profile.avatar.remove", func(t *testing.T) {
		connection.dispatch(requestFrame{
			Type:   "req",
			ID:     "3",
			Method: "profile.avatar.remove",
			Params: json.RawMessage(`{}`),
		})
		response := readRPCResponse(t, clientConnection)
		if !response.OK {
			t.Fatalf("response ok = false, error = %+v", response.Error)
		}
		payload := decodeRPCProfilePayload(t, response.Payload)
		if payload.AvatarMediaID != "" {
			t.Fatalf("avatarMediaId = %q, want empty", payload.AvatarMediaID)
		}

		persisted, err := configs.LoadProfile()
		if err != nil {
			t.Fatalf("LoadProfile failed: %v", err)
		}
		if persisted.AvatarMediaID != "" {
			t.Fatalf("persisted avatarMediaId = %q, want empty", persisted.AvatarMediaID)
		}
	})
}
