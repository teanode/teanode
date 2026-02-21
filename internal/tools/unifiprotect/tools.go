package unifiprotect

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/providers"
)

// unifiProtectTool implements the consolidated unifi_protect tool.
type unifiProtectTool struct {
	client  Client
	checker *AccessChecker
}

func (self *unifiProtectTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name: "unifi_protect",
			Description: "Interact with UniFi Protect camera system. Always list cameras first to obtain valid camera IDs. Actions: " +
				"list_cameras (list all cameras, optionally filter by isDoorbell), " +
				"get_camera (get details for a specific camera by ID or name), " +
				"get_snapshot (capture a JPEG snapshot from a camera), " +
				"set_status_light (enable/disable camera status LED), " +
				"set_recording_mode (set recording mode: always, never, detections), " +
				"set_privacy_mode (enable/disable privacy mode on a camera).",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list_cameras", "get_camera", "get_snapshot", "set_status_light", "set_recording_mode", "set_privacy_mode"},
						"description": "The UniFi Protect action to perform.",
					},
					"cameraId": map[string]interface{}{
						"type":        "string",
						"description": "Camera ID or name. Required for get_camera, get_snapshot, set_status_light, set_recording_mode, set_privacy_mode.",
					},
					"isDoorbell": map[string]interface{}{
						"type":        "boolean",
						"description": "Filter list_cameras to doorbells only (true) or non-doorbells only (false). Omit for all cameras.",
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable/disable flag for set_status_light and set_privacy_mode.",
					},
					"recordingMode": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"always", "never", "detections"},
						"description": "Recording mode for set_recording_mode action.",
					},
				},
				"required": []string{"action"},
			},
			Returns: map[string]interface{}{
				"type":        "object",
				"description": "Action-dependent JSON result.",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"description": "The action that was performed.",
					},
					"cameras": map[string]interface{}{
						"type":        "array",
						"description": "List of cameras (list_cameras).",
					},
					"camera": map[string]interface{}{
						"type":        "object",
						"description": "Camera details (get_camera).",
					},
					"base64": map[string]interface{}{
						"type":        "string",
						"description": "Base64-encoded JPEG snapshot (get_snapshot).",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Image format (get_snapshot).",
					},
					"result": map[string]interface{}{
						"type":        "string",
						"description": "Write action result message.",
					},
				},
			},
		},
	}
}

func (self *unifiProtectTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	var arguments struct {
		Action        string `json:"action"`
		CameraID      string `json:"cameraId"`
		IsDoorbell    *bool  `json:"isDoorbell"`
		Enabled       *bool  `json:"enabled"`
		RecordingMode string `json:"recordingMode"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("parsing arguments: %w", err)
	}

	switch arguments.Action {
	case "list_cameras":
		return self.executeListCameras(ctx, arguments.IsDoorbell)
	case "get_camera":
		return self.executeGetCamera(ctx, arguments.CameraID)
	case "get_snapshot":
		return self.executeGetSnapshot(ctx, arguments.CameraID)
	case "set_status_light":
		return self.executeSetStatusLight(ctx, arguments.CameraID, arguments.Enabled)
	case "set_recording_mode":
		return self.executeSetRecordingMode(ctx, arguments.CameraID, arguments.RecordingMode)
	case "set_privacy_mode":
		return self.executeSetPrivacyMode(ctx, arguments.CameraID, arguments.Enabled)
	default:
		return "", fmt.Errorf("unknown unifi_protect action: %s", arguments.Action)
	}
}

func (self *unifiProtectTool) executeListCameras(ctx context.Context, isDoorbellFilter *bool) (string, error) {
	cameras, err := self.client.GetCameras(ctx)
	if err != nil {
		return "", fmt.Errorf("listing cameras: %w", err)
	}

	type cameraSummary struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Type          string `json:"type"`
		State         string `json:"state"`
		IsConnected   bool   `json:"isConnected"`
		IsDoorbell    bool   `json:"isDoorbell"`
		RecordingMode string `json:"recordingMode"`
	}

	var filtered []cameraSummary
	for _, camera := range cameras {
		if !self.checker.IsCameraAllowed(camera.ID, camera.Name) {
			continue
		}
		if isDoorbellFilter != nil && camera.IsDoorbell != *isDoorbellFilter {
			continue
		}
		filtered = append(filtered, cameraSummary{
			ID:            camera.ID,
			Name:          camera.Name,
			Type:          camera.Type,
			State:         camera.State,
			IsConnected:   camera.IsConnected,
			IsDoorbell:    camera.IsDoorbell,
			RecordingMode: camera.RecordingMode,
		})
	}

	return marshalResult(map[string]interface{}{
		"action":  "list_cameras",
		"cameras": filtered,
		"count":   len(filtered),
	})
}

func (self *unifiProtectTool) executeGetCamera(ctx context.Context, cameraID string) (string, error) {
	if cameraID == "" {
		return "", fmt.Errorf("cameraId is required for get_camera action")
	}

	camera, err := self.resolveCamera(ctx, cameraID)
	if err != nil {
		return "", err
	}

	return marshalResult(map[string]interface{}{
		"action": "get_camera",
		"camera": camera,
	})
}

func (self *unifiProtectTool) executeGetSnapshot(ctx context.Context, cameraID string) (string, error) {
	if cameraID == "" {
		return "", fmt.Errorf("cameraId is required for get_snapshot action")
	}

	camera, err := self.resolveCamera(ctx, cameraID)
	if err != nil {
		return "", err
	}

	snapshotData, err := self.client.GetSnapshot(ctx, camera.ID)
	if err != nil {
		return "", fmt.Errorf("getting snapshot: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(snapshotData)

	return marshalResult(map[string]interface{}{
		"base64": encoded,
		"format": "jpeg",
	})
}

func (self *unifiProtectTool) executeSetStatusLight(ctx context.Context, cameraID string, enabled *bool) (string, error) {
	if err := self.checkWriteAction("set_status_light"); err != nil {
		return "", err
	}
	if cameraID == "" {
		return "", fmt.Errorf("cameraId is required for set_status_light action")
	}
	if enabled == nil {
		return "", fmt.Errorf("enabled is required for set_status_light action")
	}

	camera, err := self.resolveCamera(ctx, cameraID)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"ledSettings": map[string]interface{}{
			"isEnabled": *enabled,
		},
	}
	if err := self.client.PatchCamera(ctx, camera.ID, payload); err != nil {
		return "", fmt.Errorf("setting status light: %w", err)
	}

	return marshalResult(map[string]interface{}{
		"action":   "set_status_light",
		"cameraId": camera.ID,
		"name":     camera.Name,
		"enabled":  *enabled,
		"result":   "status light updated successfully",
	})
}

func (self *unifiProtectTool) executeSetRecordingMode(ctx context.Context, cameraID string, recordingMode string) (string, error) {
	if err := self.checkWriteAction("set_recording_mode"); err != nil {
		return "", err
	}
	if cameraID == "" {
		return "", fmt.Errorf("cameraId is required for set_recording_mode action")
	}
	if recordingMode == "" {
		return "", fmt.Errorf("recordingMode is required for set_recording_mode action")
	}

	switch recordingMode {
	case "always", "never", "detections":
		// valid
	default:
		return "", fmt.Errorf("recordingMode must be one of: always, never, detections; got %q", recordingMode)
	}

	camera, err := self.resolveCamera(ctx, cameraID)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"recordingSettings": map[string]interface{}{
			"mode": recordingMode,
		},
	}
	if err := self.client.PatchCamera(ctx, camera.ID, payload); err != nil {
		return "", fmt.Errorf("setting recording mode: %w", err)
	}

	return marshalResult(map[string]interface{}{
		"action":        "set_recording_mode",
		"cameraId":      camera.ID,
		"name":          camera.Name,
		"recordingMode": recordingMode,
		"result":        "recording mode updated successfully",
	})
}

func (self *unifiProtectTool) executeSetPrivacyMode(ctx context.Context, cameraID string, enabled *bool) (string, error) {
	if err := self.checkWriteAction("set_privacy_mode"); err != nil {
		return "", err
	}
	if cameraID == "" {
		return "", fmt.Errorf("cameraId is required for set_privacy_mode action")
	}
	if enabled == nil {
		return "", fmt.Errorf("enabled is required for set_privacy_mode action")
	}

	camera, err := self.resolveCamera(ctx, cameraID)
	if err != nil {
		return "", err
	}

	// Privacy mode in Protect is controlled via privacy zones.
	// Enabling adds a full-frame zone; disabling clears all zones.
	privacyZones := make([]interface{}, 0)
	if *enabled {
		privacyZones = []interface{}{
			map[string]interface{}{
				"id":    0,
				"name":  "Privacy",
				"color": "#85EFAC",
				"points": [][]float64{
					{0, 0},
					{1, 0},
					{1, 1},
					{0, 1},
				},
			},
		}
	}

	payload := map[string]interface{}{
		"privacyZones": privacyZones,
	}
	if err := self.client.PatchCamera(ctx, camera.ID, payload); err != nil {
		return "", fmt.Errorf("setting privacy mode: %w", err)
	}

	return marshalResult(map[string]interface{}{
		"action":   "set_privacy_mode",
		"cameraId": camera.ID,
		"name":     camera.Name,
		"enabled":  *enabled,
		"result":   "privacy mode updated successfully",
	})
}

// resolveCamera finds a camera by ID or name from the bootstrap data and
// enforces the camera allowlist.
func (self *unifiProtectTool) resolveCamera(ctx context.Context, cameraIDOrName string) (*Camera, error) {
	cameras, err := self.client.GetCameras(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching cameras: %w", err)
	}

	normalizedQuery := strings.ToLower(strings.TrimSpace(cameraIDOrName))

	for _, camera := range cameras {
		if camera.ID == cameraIDOrName || strings.ToLower(camera.Name) == normalizedQuery {
			if !self.checker.IsCameraAllowed(camera.ID, camera.Name) {
				return nil, fmt.Errorf("camera %q is not accessible (blocked by access rules)", cameraIDOrName)
			}
			return &camera, nil
		}
	}

	return nil, fmt.Errorf("camera %q not found", cameraIDOrName)
}

// checkWriteAction verifies that write operations are allowed and the specific
// action is in the dangerous actions allowlist.
func (self *unifiProtectTool) checkWriteAction(action string) error {
	if !self.checker.IsWriteAllowed() {
		return fmt.Errorf("%s is blocked: UniFi Protect is configured in read-only mode", action)
	}
	if !self.checker.IsActionAllowed(action) {
		return fmt.Errorf("%s is not in allowDangerousActions list; add %q to the UniFi Protect config to enable it", action, action)
	}
	return nil
}

// marshalResult marshals a result map to a JSON string.
func marshalResult(result map[string]interface{}) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(data), nil
}
