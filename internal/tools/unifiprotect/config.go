package unifiprotect

import (
	"strings"

	"github.com/teanode/teanode/internal/configs"
)

// AccessChecker validates whether a camera or action is permitted.
type AccessChecker struct {
	allowedCameras        map[string]bool // keyed by normalized name; nil-config = all
	allowCameraAll        bool            // true when no allowlist is configured
	allowDangerousActions map[string]bool
	readOnly              bool
}

// NewAccessChecker creates an AccessChecker from the given configuration.
func NewAccessChecker(config *configs.UniFiProtectConfig) *AccessChecker {
	checker := &AccessChecker{
		allowedCameras:        make(map[string]bool),
		allowDangerousActions: make(map[string]bool),
	}

	if config == nil {
		checker.allowCameraAll = true
		return checker
	}

	checker.readOnly = config.ReadOnly

	if len(config.AllowedCameras) == 0 {
		checker.allowCameraAll = true
	} else {
		for _, camera := range config.AllowedCameras {
			checker.allowedCameras[normalizeCamera(camera)] = true
		}
	}

	for _, action := range config.AllowDangerousActions {
		checker.allowDangerousActions[action] = true
	}

	return checker
}

// normalizeCamera lowercases and trims a camera name/ID for comparison.
func normalizeCamera(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// IsCameraAllowed checks whether a camera is accessible by name or ID.
func (self *AccessChecker) IsCameraAllowed(cameraId string, cameraName string) bool {
	if self.allowCameraAll {
		return true
	}
	if self.allowedCameras[normalizeCamera(cameraId)] {
		return true
	}
	if cameraName != "" && self.allowedCameras[normalizeCamera(cameraName)] {
		return true
	}
	return false
}

// IsWriteAllowed returns false when ReadOnly mode is enabled.
func (self *AccessChecker) IsWriteAllowed() bool {
	return !self.readOnly
}

// IsActionAllowed checks whether a specific dangerous action is permitted.
// Returns true only if NOT readOnly AND the action is in the allowDangerousActions list.
func (self *AccessChecker) IsActionAllowed(action string) bool {
	if self.readOnly {
		return false
	}
	return self.allowDangerousActions[action]
}
