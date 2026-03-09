package runners

import (
	"context"
	"strings"
	"testing"
)

func TestBuildVoiceOverlay_NoMode(t *testing.T) {
	ctx := context.Background()
	result := buildVoiceOverlay(ctx)
	if result != "" {
		t.Fatalf("expected empty, got %q", result)
	}
}

func TestBuildVoiceOverlay_Call(t *testing.T) {
	ctx := ContextWithVoiceMode(context.Background(), VoiceModeCall)
	result := buildVoiceOverlay(ctx)
	if !strings.Contains(result, "<voice-call>") {
		t.Error("missing <voice-call> tag")
	}
	if !strings.Contains(result, "live voice call") {
		t.Error("missing voice call description")
	}
	if !strings.Contains(result, "</voice-call>") {
		t.Error("missing closing tag")
	}
}

func TestBuildVoiceOverlay_Input(t *testing.T) {
	ctx := ContextWithVoiceMode(context.Background(), VoiceModeInput)
	result := buildVoiceOverlay(ctx)
	if !strings.Contains(result, "<voice-input>") {
		t.Error("missing <voice-input> tag")
	}
	if !strings.Contains(result, "voice input") {
		t.Error("missing voice input description")
	}
	if !strings.Contains(result, "</voice-input>") {
		t.Error("missing closing tag")
	}
}

func TestBuildVoiceOverlay_UnknownMode(t *testing.T) {
	ctx := ContextWithVoiceMode(context.Background(), "unknown")
	result := buildVoiceOverlay(ctx)
	if result != "" {
		t.Fatalf("expected empty for unknown mode, got %q", result)
	}
}
