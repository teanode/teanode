package scoring

import (
	"testing"
	"time"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func TestCompute(t *testing.T) {
	t.Parallel()
	now := time.Now()
	events := []model.TimelineEvent{
		{At: now, Type: model.EventSpeechEnded},
		{At: now.Add(500 * time.Millisecond), Type: model.EventTranscriptFinal, Text: "hello"},
		{At: now.Add(700 * time.Millisecond), Type: model.EventResponseStarted},
		{At: now.Add(800 * time.Millisecond), Type: model.EventTTSInput, Text: "one sentence"},
		{At: now.Add(900 * time.Millisecond), Type: model.EventBargeInTriggered},
	}
	metrics := Compute(events)
	if metrics["transcriptCount"].(int64) != 1 {
		t.Fatalf("unexpected transcriptCount: %#v", metrics)
	}
	if metrics["bargeInCount"].(int64) != 1 {
		t.Fatalf("unexpected bargeInCount: %#v", metrics)
	}
	if metrics["latencySpeechEndToTranscriptMs"].(int64) <= 0 {
		t.Fatalf("expected positive latency metric: %#v", metrics)
	}
}
