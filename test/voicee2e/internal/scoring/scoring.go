package scoring

import (
	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func Compute(events []model.TimelineEvent) map[string]any {
	metrics := map[string]any{
		"transcriptCount":  int64(0),
		"responseCount":    int64(0),
		"bargeInCount":     int64(0),
		"ttsSentenceCount": int64(0),
		"ttsCharCount":     int64(0),
	}

	var speechEndedAt int64
	for _, event := range events {
		switch event.Type {
		case model.EventSpeechEnded:
			speechEndedAt = event.At.UnixMilli()
		case model.EventTranscriptFinal:
			metrics["transcriptCount"] = metrics["transcriptCount"].(int64) + 1
			if speechEndedAt > 0 {
				metrics["latencySpeechEndToTranscriptMs"] = event.At.UnixMilli() - speechEndedAt
				speechEndedAt = 0
			}
		case model.EventResponseStarted:
			metrics["responseCount"] = metrics["responseCount"].(int64) + 1
		case model.EventTurnCommitted:
			// In voice mode, a committed turn means response generation has started
			// even if downstream audio start events arrive after scenario cutoff.
			metrics["responseCount"] = metrics["responseCount"].(int64) + 1
		case model.EventBargeInTriggered:
			metrics["bargeInCount"] = metrics["bargeInCount"].(int64) + 1
		case model.EventTTSInput:
			metrics["ttsSentenceCount"] = metrics["ttsSentenceCount"].(int64) + 1
			metrics["ttsCharCount"] = metrics["ttsCharCount"].(int64) + int64(len(event.Text))
		}
	}

	return metrics
}
