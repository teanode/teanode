// Package allowlist provides helpers for allowlist-based filtering.
package allowlist

// IsAllowed checks whether a name is present in an allow list.
// A nil or empty list means everything is allowed.
func IsAllowed(name string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, entry := range allowed {
		if entry == name {
			return true
		}
	}
	return false
}
