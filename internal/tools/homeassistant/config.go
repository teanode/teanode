package homeassistant

import (
	"strings"
)

// DefaultAllowedDomains is the safe set of domains permitted by default.
var DefaultAllowedDomains = []string{
	"light",
	"switch",
	"scene",
	"climate",
	"sensor",
	"binary_sensor",
	"media_player",
	"fan",
	"cover",
	"vacuum",
	"automation",
	"input_boolean",
	"input_number",
	"input_select",
	"input_text",
	"number",
	"select",
	"weather",
	"person",
	"zone",
	"sun",
}

// DefaultBlockedDomains is the set of domains blocked by default for safety.
var DefaultBlockedDomains = []string{
	"lock",
	"alarm_control_panel",
}

// AccessChecker validates whether a given entity or domain is permitted.
type AccessChecker struct {
	allowedDomains  map[string]bool
	blockedDomains  map[string]bool
	allowedEntities map[string]bool
	readOnly        bool
}

// NewAccessChecker creates an AccessChecker from the given configuration.
// If config is nil, safe defaults are used.
func NewAccessChecker(config *resolvedConfiguration) *AccessChecker {
	checker := &AccessChecker{
		allowedDomains:  make(map[string]bool),
		blockedDomains:  make(map[string]bool),
		allowedEntities: make(map[string]bool),
	}

	allowedDomains := DefaultAllowedDomains
	blockedDomains := DefaultBlockedDomains

	if config != nil {
		checker.readOnly = config.readOnly
		if config.allowedDomains != nil {
			allowedDomains = config.allowedDomains
		}
		if config.blockedDomains != nil {
			blockedDomains = config.blockedDomains
		}
		for _, entity := range config.allowedEntities {
			checker.allowedEntities[entity] = true
		}
	}

	for _, domain := range allowedDomains {
		checker.allowedDomains[domain] = true
	}
	for _, domain := range blockedDomains {
		checker.blockedDomains[domain] = true
	}

	return checker
}

// DomainOf extracts the domain prefix from an entity ID (e.g. "light" from "light.living_room").
func DomainOf(entityId string) string {
	if index := strings.Index(entityId, "."); index > 0 {
		return entityId[:index]
	}
	return entityId
}

// IsEntityAllowed checks whether a specific entity is accessible.
func (self *AccessChecker) IsEntityAllowed(entityId string) bool {
	domain := DomainOf(entityId)

	// Blocked domains always take priority.
	if self.blockedDomains[domain] {
		return false
	}

	// If an explicit entity allowlist is configured, only those entities pass.
	if len(self.allowedEntities) > 0 {
		return self.allowedEntities[entityId]
	}

	// Otherwise check the domain allowlist.
	return self.allowedDomains[domain]
}

// IsDomainAllowed checks whether a domain is accessible.
func (self *AccessChecker) IsDomainAllowed(domain string) bool {
	if self.blockedDomains[domain] {
		return false
	}
	return self.allowedDomains[domain]
}

// IsWriteAllowed returns false when ReadOnly mode is enabled.
func (self *AccessChecker) IsWriteAllowed() bool {
	return !self.readOnly
}
