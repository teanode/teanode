package datetime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/tools"
)

// fixedClock returns a function that always returns the given time.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestDatetimeToolExecute(t *testing.T) {
	tool := &datetimeTool{}
	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Result should contain today's date.
	today := time.Now().Format("2006-01-02")
	if !strings.Contains(result, today) {
		t.Errorf("result %q should contain today's date %s", result, today)
	}

	// Result should contain timezone.
	tz := time.Now().Format("MST")
	if !strings.Contains(result, tz) {
		t.Errorf("result %q should contain timezone %s", result, tz)
	}
}

func TestDatetimeToolDefinition(t *testing.T) {
	tool := &datetimeTool{}
	def := tool.Definition()
	if def.Function.Name != "datetime" {
		t.Errorf("name = %q, want datetime", def.Function.Name)
	}
	if !strings.Contains(def.Function.Description, "already provided automatically") {
		t.Errorf("description should mention datetime is already in context, got %q", def.Function.Description)
	}
}

func TestBuildOverlayFormat(t *testing.T) {
	loc := time.FixedZone("PDT", -7*3600)
	frozen := time.Date(2026, 3, 11, 14, 32, 7, 0, loc)
	original := clock
	clock = fixedClock(frozen)
	defer func() { clock = original }()

	tool := &datetimeTool{}
	overlay, err := tool.BuildOverlay(context.Background())
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}

	want := "<current_datetime>\n2026-03-11 14:32:07 PDT (UTC-07:00)\n</current_datetime>"
	if overlay != want {
		t.Errorf("overlay mismatch\n got: %q\nwant: %q", overlay, want)
	}
}

func TestBuildOverlayPositiveOffset(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+30*60)
	frozen := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	original := clock
	clock = fixedClock(frozen)
	defer func() { clock = original }()

	tool := &datetimeTool{}
	overlay, err := tool.BuildOverlay(context.Background())
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}

	want := "<current_datetime>\n2026-06-15 09:00:00 IST (UTC+05:30)\n</current_datetime>"
	if overlay != want {
		t.Errorf("overlay mismatch\n got: %q\nwant: %q", overlay, want)
	}
}

func TestBuildOverlayUTC(t *testing.T) {
	frozen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	original := clock
	clock = fixedClock(frozen)
	defer func() { clock = original }()

	tool := &datetimeTool{}
	overlay, err := tool.BuildOverlay(context.Background())
	if err != nil {
		t.Fatalf("BuildOverlay: %v", err)
	}

	want := "<current_datetime>\n2026-01-01 00:00:00 UTC (UTC+00:00)\n</current_datetime>"
	if overlay != want {
		t.Errorf("overlay mismatch\n got: %q\nwant: %q", overlay, want)
	}
}

func TestBuildOverlayInRegistry(t *testing.T) {
	// Verify overlay appears when collected through the registry.
	loc := time.FixedZone("EST", -5*3600)
	frozen := time.Date(2026, 12, 25, 18, 30, 0, 0, loc)
	original := clock
	clock = fixedClock(frozen)
	defer func() { clock = original }()

	registry := tools.NewEmptyToolRegistry()
	registry.Register(&datetimeTool{})

	overlays := registry.BuildOverlays(context.Background())
	if len(overlays) != 1 {
		t.Fatalf("expected 1 overlay, got %d", len(overlays))
	}
	if !strings.Contains(overlays[0], "<current_datetime>") {
		t.Errorf("overlay should contain <current_datetime> tag, got %q", overlays[0])
	}
	if !strings.Contains(overlays[0], "2026-12-25 18:30:00 EST") {
		t.Errorf("overlay should contain formatted time, got %q", overlays[0])
	}
}
