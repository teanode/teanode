package timeutil

import (
	"os"
	"strings"
	"time"
)

// LocalLocation returns the current system timezone by re-reading system
// configuration. Unlike time.Local, which is cached once at process startup,
// this function detects timezone changes made while the process is running.
func LocalLocation() *time.Location {
	// 1. Honour the TZ environment variable (same precedence as Go runtime).
	if tz := os.Getenv("TZ"); tz != "" {
		if tz == "UTC" || tz == "utc" {
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
