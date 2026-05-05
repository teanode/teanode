package timeutil

import (
	"testing"
	"time"
)

func TestLocalLocationReturnsTZEnvVar(t *testing.T) {
	t.Setenv("TZ", "America/Chicago")
	loc := LocalLocation()

	// Verify the returned location matches the TZ env var.
	expected, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// Compare by formatting the same instant in both locations.
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	if loc.String() != expected.String() {
		t.Errorf("LocalLocation() = %q, want %q", loc.String(), expected.String())
	}
	if now.In(loc).Format("MST") != now.In(expected).Format("MST") {
		t.Errorf("zone abbreviation mismatch: got %q, want %q",
			now.In(loc).Format("MST"), now.In(expected).Format("MST"))
	}
}

func TestLocalLocationUTC(t *testing.T) {
	t.Setenv("TZ", "UTC")
	loc := LocalLocation()
	if loc != time.UTC {
		t.Errorf("LocalLocation() = %v, want UTC", loc)
	}
}

func TestLocalLocationFallsBackGracefully(t *testing.T) {
	// With no TZ set and (possibly) no /etc/localtime, should not panic.
	t.Setenv("TZ", "")
	loc := LocalLocation()
	if loc == nil {
		t.Fatal("LocalLocation() returned nil")
	}
}

func TestZoneNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/share/zoneinfo/America/New_York", "America/New_York"},
		{"/usr/share/zoneinfo/Europe/London", "Europe/London"},
		{"../zoneinfo/Asia/Tokyo", "Asia/Tokyo"},
		{"/some/other/path", ""},
	}
	for _, tt := range tests {
		got := zoneNameFromPath(tt.path)
		if got != tt.want {
			t.Errorf("zoneNameFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
