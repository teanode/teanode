package scoring

import (
	"strings"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func Compute(events []model.TimelineEvent) map[string]any {
	metrics := map[string]any{
		"transcript_count":   int64(0),
		"response_count":     int64(0),
		"barge_in_count":     int64(0),
		"tts_sentence_count": int64(0),
		"tts_char_count":     int64(0),
	}

	var speechEndedAt int64
	for _, event := range events {
		switch event.Type {
		case model.EventSpeechEnded:
			speechEndedAt = event.At.UnixMilli()
		case model.EventTranscriptFinal:
			metrics["transcript_count"] = metrics["transcript_count"].(int64) + 1
			if speechEndedAt > 0 {
				metrics["latency_speech_end_to_transcript_ms"] = event.At.UnixMilli() - speechEndedAt
				speechEndedAt = 0
			}
		case model.EventResponseStarted:
			metrics["response_count"] = metrics["response_count"].(int64) + 1
		case model.EventTurnCommitted:
			// In voice mode, a committed turn means response generation has started
			// even if downstream audio start events arrive after scenario cutoff.
			metrics["response_count"] = metrics["response_count"].(int64) + 1
		case model.EventBargeInTriggered:
			metrics["barge_in_count"] = metrics["barge_in_count"].(int64) + 1
		case model.EventTTSInput:
			metrics["tts_sentence_count"] = metrics["tts_sentence_count"].(int64) + 1
			metrics["tts_char_count"] = metrics["tts_char_count"].(int64) + int64(len(strings.TrimSpace(event.Text)))
		}
	}

	return metrics
}
