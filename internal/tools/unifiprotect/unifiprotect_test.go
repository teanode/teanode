package unifiprotect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	toolregistry "github.com/teanode/teanode/internal/tools"
)

// --- mock client ---

type mockClient struct {
	cameras       []Camera
	snapshotData  []byte
	patchError    error
	getCamerasErr error

	// track calls
	patchCalls []patchCall
}

type patchCall struct {
	CameraID string
	Payload  map[string]interface{}
}

func (self *mockClient) GetCameras(ctx context.Context) ([]Camera, error) {
	if self.getCamerasErr != nil {
		return nil, self.getCamerasErr
	}
	return self.cameras, nil
}

func (self *mockClient) GetSnapshot(ctx context.Context, cameraId string) ([]byte, error) {
	if self.getCamerasErr != nil {
		return nil, self.getCamerasErr
	}
	if self.snapshotData != nil {
		return self.snapshotData, nil
	}
	return []byte{0xFF, 0xD8, 0xFF, 0xE0}, nil // minimal JPEG header
}

func (self *mockClient) PatchCamera(ctx context.Context, cameraId string, payload map[string]interface{}) error {
	if self.patchError != nil {
		return self.patchError
	}
	self.patchCalls = append(self.patchCalls, patchCall{
		CameraID: cameraId,
		Payload:  payload,
	})
	return nil
}

func newMockClient() *mockClient {
	return &mockClient{
		cameras: []Camera{
			{ID: "cam001", Name: "Front Door", Type: "UVC G4 Pro", State: "CONNECTED", IsConnected: true, IsDoorbell: false, RecordingMode: "always"},
			{ID: "cam002", Name: "Backyard", Type: "UVC G4 Bullet", State: "CONNECTED", IsConnected: true, IsDoorbell: false, RecordingMode: "detections"},
			{ID: "cam003", Name: "Doorbell", Type: "UVC G4 Doorbell", State: "CONNECTED", IsConnected: true, IsDoorbell: true, RecordingMode: "always"},
		},
	}
}

func newTestTool(client *mockClient, config *resolvedConfig) *unifiProtectExecution {
	if config == nil {
		config = &resolvedConfig{}
	}
	return &unifiProtectExecution{
		client:  client,
		checker: NewAccessChecker(config),
	}
}

// --- AccessChecker tests ---

func TestAccessChecker_AllCamerasAllowed(testing *testing.T) {
	checker := NewAccessChecker(nil)

	if !checker.IsCameraAllowed("cam001", "Front Door") {
		testing.Error("expected all cameras allowed with nil config")
	}
	if !checker.IsCameraAllowed("cam999", "Unknown") {
		testing.Error("expected all cameras allowed with nil config")
	}
}

func TestAccessChecker_AllowedCamerasByName(testing *testing.T) {
	config := &resolvedConfig{
		allowedCameras: []string{"Front Door", "Backyard"},
	}
	checker := NewAccessChecker(config)

	if !checker.IsCameraAllowed("cam001", "Front Door") {
		testing.Error("expected Front Door to be allowed")
	}
	if !checker.IsCameraAllowed("cam002", "Backyard") {
		testing.Error("expected Backyard to be allowed")
	}
	if checker.IsCameraAllowed("cam003", "Doorbell") {
		testing.Error("expected Doorbell to be blocked")
	}
}

func TestAccessChecker_AllowedCamerasByID(testing *testing.T) {
	config := &resolvedConfig{
		allowedCameras: []string{"cam001"},
	}
	checker := NewAccessChecker(config)

	if !checker.IsCameraAllowed("cam001", "Front Door") {
		testing.Error("expected cam001 to be allowed by ID")
	}
	if checker.IsCameraAllowed("cam002", "Backyard") {
		testing.Error("expected cam002 to be blocked")
	}
}

func TestAccessChecker_CaseInsensitive(testing *testing.T) {
	config := &resolvedConfig{
		allowedCameras: []string{"front door"},
	}
	checker := NewAccessChecker(config)

	if !checker.IsCameraAllowed("cam001", "Front Door") {
		testing.Error("expected case-insensitive name match")
	}
}

func TestAccessChecker_EmptyAllowlist(testing *testing.T) {
	config := &resolvedConfig{
		allowedCameras: []string{},
	}
	checker := NewAccessChecker(config)

	if !checker.IsCameraAllowed("cam001", "Front Door") {
		testing.Error("expected all cameras allowed with empty allowlist")
	}
}

func TestAccessChecker_ReadOnly(testing *testing.T) {
	config := &resolvedConfig{readOnly: true}
	checker := NewAccessChecker(config)

	if checker.IsWriteAllowed() {
		testing.Error("expected write to be blocked in read-only mode")
	}

	config2 := &resolvedConfig{readOnly: false}
	checker2 := NewAccessChecker(config2)
	if !checker2.IsWriteAllowed() {
		testing.Error("expected write to be allowed when not read-only")
	}
}

func TestAccessChecker_IsActionAllowed(testing *testing.T) {
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_status_light", "set_recording_mode"},
	}
	checker := NewAccessChecker(config)

	if !checker.IsActionAllowed("set_status_light") {
		testing.Error("expected set_status_light to be allowed")
	}
	if !checker.IsActionAllowed("set_recording_mode") {
		testing.Error("expected set_recording_mode to be allowed")
	}
	if checker.IsActionAllowed("set_privacy_mode") {
		testing.Error("expected set_privacy_mode to be blocked (not in list)")
	}
}

func TestAccessChecker_ReadOnlyBlocksActions(testing *testing.T) {
	config := &resolvedConfig{
		readOnly:              true,
		allowDangerousActions: []string{"set_status_light"},
	}
	checker := NewAccessChecker(config)

	if checker.IsActionAllowed("set_status_light") {
		testing.Error("expected action to be blocked in read-only mode even if in allowlist")
	}
}

func TestAccessChecker_NilConfig(testing *testing.T) {
	checker := NewAccessChecker(nil)

	if !checker.IsCameraAllowed("cam001", "Any Camera") {
		testing.Error("expected all cameras allowed with nil config")
	}
	if !checker.IsWriteAllowed() {
		testing.Error("expected write allowed with nil config")
	}
	if checker.IsActionAllowed("set_status_light") {
		testing.Error("expected no actions allowed with nil config (empty allowlist)")
	}
}

// --- Tool Definition tests ---

func TestToolDefinition(testing *testing.T) {
	definition := (&unifiProtectTool{}).Definition()

	if definition.Type != "function" {
		testing.Errorf("expected type 'function', got %q", definition.Type)
	}
	if definition.Function.Name != "unifi_protect" {
		testing.Errorf("expected name 'unifi_protect', got %q", definition.Function.Name)
	}
	if definition.Function.Description == "" {
		testing.Error("expected non-empty description")
	}
	if definition.Function.Parameters == nil {
		testing.Error("expected non-nil parameters")
	}
	if definition.Function.Returns == nil {
		testing.Error("expected non-nil returns")
	}

	// Verify action enum.
	params := definition.Function.Parameters.(map[string]interface{})
	properties := params["properties"].(map[string]interface{})
	action := properties["action"].(map[string]interface{})
	actionEnum := action["enum"].([]string)
	expectedActions := []string{"list_cameras", "get_camera", "get_snapshot", "set_status_light", "set_recording_mode", "set_privacy_mode"}
	if len(actionEnum) != len(expectedActions) {
		testing.Fatalf("expected %d actions, got %d", len(expectedActions), len(actionEnum))
	}
	for index, want := range expectedActions {
		if actionEnum[index] != want {
			testing.Errorf("action[%d] = %q, want %q", index, actionEnum[index], want)
		}
	}
}

// --- list_cameras tests ---

func TestListCameras_Basic(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_cameras"})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		testing.Fatalf("invalid JSON response: %v", err)
	}

	if response["action"] != "list_cameras" {
		testing.Errorf("expected action 'list_cameras', got %v", response["action"])
	}

	cameras := response["cameras"].([]interface{})
	if len(cameras) != 3 {
		testing.Fatalf("expected 3 cameras, got %d", len(cameras))
	}
}

func TestListCameras_FilterDoorbell(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_cameras", "isDoorbell": true})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	cameras := response["cameras"].([]interface{})
	if len(cameras) != 1 {
		testing.Fatalf("expected 1 doorbell, got %d", len(cameras))
	}

	firstCamera := cameras[0].(map[string]interface{})
	if firstCamera["name"] != "Doorbell" {
		testing.Errorf("expected doorbell camera, got %v", firstCamera["name"])
	}
}

func TestListCameras_FilterNonDoorbell(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_cameras", "isDoorbell": false})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	cameras := response["cameras"].([]interface{})
	if len(cameras) != 2 {
		testing.Fatalf("expected 2 non-doorbell cameras, got %d", len(cameras))
	}
}

func TestListCameras_AllowlistFilter(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowedCameras: []string{"Front Door"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_cameras"})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	cameras := response["cameras"].([]interface{})
	if len(cameras) != 1 {
		testing.Fatalf("expected 1 camera (allowlist filter), got %d", len(cameras))
	}
}

func TestListCameras_ClientError(testing *testing.T) {
	client := newMockClient()
	client.getCamerasErr = fmt.Errorf("connection refused")
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "list_cameras"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil {
		testing.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "listing cameras") {
		testing.Errorf("expected 'listing cameras' in error, got: %v", err)
	}
}

// --- get_camera tests ---

func TestGetCamera_ByID(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_camera", "cameraId": "cam001"})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "get_camera" {
		testing.Errorf("expected action 'get_camera', got %v", response["action"])
	}
	camera := response["camera"].(map[string]interface{})
	if camera["id"] != "cam001" {
		testing.Errorf("expected camera id 'cam001', got %v", camera["id"])
	}
	if camera["name"] != "Front Door" {
		testing.Errorf("expected camera name 'Front Door', got %v", camera["name"])
	}
}

func TestGetCamera_ByName(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_camera", "cameraId": "Backyard"})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	camera := response["camera"].(map[string]interface{})
	if camera["id"] != "cam002" {
		testing.Errorf("expected camera id 'cam002', got %v", camera["id"])
	}
}

func TestGetCamera_NotFound(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_camera", "cameraId": "nonexistent"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		testing.Errorf("expected not found error, got: %v", err)
	}
}

func TestGetCamera_BlockedByAllowlist(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowedCameras: []string{"Front Door"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_camera", "cameraId": "Backyard"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		testing.Errorf("expected access denied error, got: %v", err)
	}
}

func TestGetCamera_MissingCameraID(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_camera"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "cameraId is required") {
		testing.Errorf("expected 'cameraId is required' error, got: %v", err)
	}
}

// --- get_snapshot tests ---

func TestGetSnapshot_Basic(testing *testing.T) {
	client := newMockClient()
	client.snapshotData = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_snapshot", "cameraId": "cam001"})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["format"] != "jpeg" {
		testing.Errorf("expected format 'jpeg', got %v", response["format"])
	}
	base64Data, ok := response["base64"].(string)
	if !ok || base64Data == "" {
		testing.Error("expected non-empty base64 data")
	}
}

func TestGetSnapshot_ByName(testing *testing.T) {
	client := newMockClient()
	tool := newTestTool(client, nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_snapshot", "cameraId": "Front Door"})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["format"] != "jpeg" {
		testing.Errorf("expected format 'jpeg', got %v", response["format"])
	}
}

func TestGetSnapshot_BlockedCamera(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowedCameras: []string{"Backyard"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_snapshot", "cameraId": "Front Door"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		testing.Errorf("expected access denied error, got: %v", err)
	}
}

func TestGetSnapshot_MissingCameraID(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "get_snapshot"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "cameraId is required") {
		testing.Errorf("expected 'cameraId is required' error, got: %v", err)
	}
}

// --- set_status_light tests ---

func TestSetStatusLight_Allowed(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_status_light"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_status_light",
		"cameraId": "cam001",
		"enabled":  true,
	})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "set_status_light" {
		testing.Errorf("expected action 'set_status_light', got %v", response["action"])
	}
	if response["enabled"] != true {
		testing.Errorf("expected enabled true, got %v", response["enabled"])
	}

	if len(client.patchCalls) != 1 {
		testing.Fatalf("expected 1 patch call, got %d", len(client.patchCalls))
	}
	if client.patchCalls[0].CameraID != "cam001" {
		testing.Errorf("expected patch on cam001, got %v", client.patchCalls[0].CameraID)
	}
}

func TestSetStatusLight_ReadOnlyBlocked(testing *testing.T) {
	config := &resolvedConfig{
		readOnly:              true,
		allowDangerousActions: []string{"set_status_light"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_status_light",
		"cameraId": "cam001",
		"enabled":  true,
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		testing.Errorf("expected read-only error, got: %v", err)
	}
}

func TestSetStatusLight_NotInAllowlist(testing *testing.T) {
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_recording_mode"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_status_light",
		"cameraId": "cam001",
		"enabled":  true,
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "allowDangerousActions") {
		testing.Errorf("expected allowDangerousActions error, got: %v", err)
	}
}

func TestSetStatusLight_MissingEnabled(testing *testing.T) {
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_status_light"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_status_light",
		"cameraId": "cam001",
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "enabled is required") {
		testing.Errorf("expected 'enabled is required' error, got: %v", err)
	}
}

// --- set_recording_mode tests ---

func TestSetRecordingMode_Allowed(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_recording_mode"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":        "set_recording_mode",
		"cameraId":      "cam001",
		"recordingMode": "detections",
	})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "set_recording_mode" {
		testing.Errorf("expected action 'set_recording_mode', got %v", response["action"])
	}
	if response["recordingMode"] != "detections" {
		testing.Errorf("expected recordingMode 'detections', got %v", response["recordingMode"])
	}

	if len(client.patchCalls) != 1 {
		testing.Fatalf("expected 1 patch call, got %d", len(client.patchCalls))
	}
}

func TestSetRecordingMode_InvalidMode(testing *testing.T) {
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_recording_mode"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":        "set_recording_mode",
		"cameraId":      "cam001",
		"recordingMode": "invalid",
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "must be one of") {
		testing.Errorf("expected invalid mode error, got: %v", err)
	}
}

func TestSetRecordingMode_ReadOnlyBlocked(testing *testing.T) {
	config := &resolvedConfig{
		readOnly:              true,
		allowDangerousActions: []string{"set_recording_mode"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":        "set_recording_mode",
		"cameraId":      "cam001",
		"recordingMode": "always",
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		testing.Errorf("expected read-only error, got: %v", err)
	}
}

func TestSetRecordingMode_MissingMode(testing *testing.T) {
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_recording_mode"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_recording_mode",
		"cameraId": "cam001",
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "recordingMode is required") {
		testing.Errorf("expected 'recordingMode is required' error, got: %v", err)
	}
}

// --- set_privacy_mode tests ---

func TestSetPrivacyMode_Enable(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_privacy_mode"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_privacy_mode",
		"cameraId": "cam001",
		"enabled":  true,
	})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["action"] != "set_privacy_mode" {
		testing.Errorf("expected action 'set_privacy_mode', got %v", response["action"])
	}
	if response["enabled"] != true {
		testing.Errorf("expected enabled true, got %v", response["enabled"])
	}

	if len(client.patchCalls) != 1 {
		testing.Fatalf("expected 1 patch call, got %d", len(client.patchCalls))
	}
	// Verify privacy zones were set.
	payload := client.patchCalls[0].Payload
	zones, ok := payload["privacyZones"].([]interface{})
	if !ok || len(zones) != 1 {
		testing.Errorf("expected 1 privacy zone, got %v", payload["privacyZones"])
	}
}

func TestSetPrivacyMode_Disable(testing *testing.T) {
	client := newMockClient()
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_privacy_mode"},
	}
	tool := newTestTool(client, config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_privacy_mode",
		"cameraId": "cam001",
		"enabled":  false,
	})
	result, err := tool.execute(context.Background(), string(arguments))
	if err != nil {
		testing.Fatalf("unexpected error: %v", err)
	}

	var response map[string]interface{}
	json.Unmarshal([]byte(result), &response)

	if response["enabled"] != false {
		testing.Errorf("expected enabled false, got %v", response["enabled"])
	}

	// Verify privacy zones were cleared (empty array).
	payload := client.patchCalls[0].Payload
	zones, ok := payload["privacyZones"].([]interface{})
	if !ok || len(zones) != 0 {
		testing.Errorf("expected empty privacy zones for disable, got %v", payload["privacyZones"])
	}
}

func TestSetPrivacyMode_NotInAllowlist(testing *testing.T) {
	config := &resolvedConfig{
		allowDangerousActions: []string{"set_status_light"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_privacy_mode",
		"cameraId": "cam001",
		"enabled":  true,
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "allowDangerousActions") {
		testing.Errorf("expected allowDangerousActions error, got: %v", err)
	}
}

func TestSetPrivacyMode_ReadOnlyBlocked(testing *testing.T) {
	config := &resolvedConfig{
		readOnly:              true,
		allowDangerousActions: []string{"set_privacy_mode"},
	}
	tool := newTestTool(newMockClient(), config)

	arguments, _ := json.Marshal(map[string]interface{}{
		"action":   "set_privacy_mode",
		"cameraId": "cam001",
		"enabled":  true,
	})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "read-only mode") {
		testing.Errorf("expected read-only error, got: %v", err)
	}
}

// --- unknown action tests ---

func TestUnknownAction(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	arguments, _ := json.Marshal(map[string]interface{}{"action": "unknown"})
	_, err := tool.execute(context.Background(), string(arguments))
	if err == nil || !strings.Contains(err.Error(), "unknown unifi_protect action") {
		testing.Errorf("expected unknown action error, got: %v", err)
	}
}

func TestInvalidJSON(testing *testing.T) {
	tool := newTestTool(newMockClient(), nil)

	_, err := tool.execute(context.Background(), "not json")
	if err == nil || !strings.Contains(err.Error(), "parsing arguments") {
		testing.Errorf("expected parsing error, got: %v", err)
	}
}

// --- RegisterTools tests ---

func TestRegisterTools(testing *testing.T) {
	registry := toolregistry.NewToolRegistry()
	RegisterTools(registry)
	if registry.Get("unifi_protect") == nil {
		testing.Error("expected unifi_protect tool to be registered")
	}
}
