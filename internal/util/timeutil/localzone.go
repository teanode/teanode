package timeutil

import (
	"os"
	"strings"
	"sync"
	"time"
)

const cacheTTL = 5 * time.Second

var (
	cacheMutex     sync.Mutex
	cachedLocation *time.Location
	cachedAt       time.Time
)

// LocalLocation returns the current system timezone by re-reading system
// configuration. Unlike time.Local, which is cached once at process startup,
// this function detects timezone changes made while the process is running.
//
// Results are cached for 5 seconds to avoid repeated filesystem I/O in hot
// paths (e.g. timestamp serialization). Timezone changes are still detected
// within one cache TTL.
func LocalLocation() *time.Location {
	now := time.Now()

	cacheMutex.Lock()
	if cachedLocation != nil && now.Sub(cachedAt) < cacheTTL {
		location := cachedLocation
		cacheMutex.Unlock()
		return location
	}
	cacheMutex.Unlock()

	location := resolveLocalLocation()

	cacheMutex.Lock()
	cachedLocation = location
	cachedAt = now
	cacheMutex.Unlock()

	return location
}

// InvalidateLocationCache forces the next call to LocalLocation to re-read the
// system timezone. Intended for use in tests.
func InvalidateLocationCache() {
	cacheMutex.Lock()
	cachedLocation = nil
	cachedAt = time.Time{}
	cacheMutex.Unlock()
}

func resolveLocalLocation() *time.Location {
	// 1. Honour the TZ environment variable (same precedence as Go runtime).
	// Go semantics: unset → system default, "" → UTC, value → LoadLocation.
	if tz, ok := os.LookupEnv("TZ"); ok {
		if tz == "" || tz == "UTC" || tz == "utc" {
			return time.UTC
		}
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}

	// 2. Try to resolve the /etc/localtime symlink to extract the IANA name.
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if name := zoneNameFromPath(target); name != "" {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
		}
	}

	// 3. Debian/Ubuntu store the timezone name in /etc/timezone.
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		name := strings.TrimSpace(string(data))
		if name != "" {
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
		}
	}

	// 4. Fallback: use Go's cached Local (better than nothing).
	return time.Local
}

// zoneNameFromPath extracts an IANA timezone name from a zoneinfo file path.
// Example: "/usr/share/zoneinfo/America/New_York" -> "America/New_York"
func zoneNameFromPath(path string) string {
	const marker = "zoneinfo/"
	if idx := strings.Index(path, marker); idx != -1 {
		return path[idx+len(marker):]
	}
	return ""
}
