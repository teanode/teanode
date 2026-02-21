package assertions

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func EnrichMetrics(scenario model.ScenarioSpec, timeline []model.TimelineEvent, metrics map[string]any) {
	expected := make([]string, 0, len(scenario.Audio))
	for _, step := range scenario.Audio {
		if strings.TrimSpace(step.ExpectedText) != "" {
			expected = append(expected, step.ExpectedText)
		}
	}
	if len(expected) == 0 {
		return
	}

	actual := make([]string, 0, 8)
	for _, event := range timeline {
		if event.Type == model.EventTranscriptFinal {
			text := strings.TrimSpace(event.Text)
			if text != "" {
				actual = append(actual, text)
			}
		}
	}
	score := similarity(strings.Join(expected, " "), strings.Join(actual, " "))
	metrics["transcript_similarity"] = score
}

func Evaluate(scenario model.ScenarioSpec, timeline []model.TimelineEvent, metrics map[string]any) (failures []string, warnings []string) {
	transcriptCount, _ := metrics["transcript_count"].(int64)
	if transcriptCount == 0 {
		failures = append(failures, "no transcript.final events")
	}

	responseCount, _ := metrics["response_count"].(int64)
	if responseCount == 0 {
		failures = append(failures, "no response.started events")
	}

	if scenario.Expect.RequireBargeIn {
		bargeCount, _ := metrics["barge_in_count"].(int64)
		if bargeCount == 0 {
			failures = append(failures, "expected barge-in event was not observed")
		}
	}

	if scenario.Expect.MaxResponseLatencyMS > 0 {
		if v, ok := metrics["latency_speech_end_to_transcript_ms"].(int64); ok && v > scenario.Expect.MaxResponseLatencyMS {
			failures = append(failures, fmt.Sprintf("speech_end->transcript latency too high: %dms > %dms", v, scenario.Expect.MaxResponseLatencyMS))
		}
	}
	if scenario.Expect.MinTranscriptSimilarity > 0 {
		if score, ok := metrics["transcript_similarity"].(float64); ok {
			if score < scenario.Expect.MinTranscriptSimilarity {
				failures = append(failures, fmt.Sprintf("transcript similarity too low: %.2f < %.2f", score, scenario.Expect.MinTranscriptSimilarity))
			}
		} else {
			warnings = append(warnings, "transcript similarity unavailable (missing expected_text fixtures)")
		}
	}

	if scenario.Expect.MaxResponseSentences > 0 {
		if v, ok := metrics["tts_sentence_count"].(int64); ok && int(v) > scenario.Expect.MaxResponseSentences {
			warnings = append(warnings, fmt.Sprintf("tts sentence count exceeded soft bound: %d > %d", v, scenario.Expect.MaxResponseSentences))
		}
	}

	_ = timeline
	return failures, warnings
}

var wordRE = regexp.MustCompile(`[a-z0-9]+`)

func similarity(expected, actual string) float64 {
	ew := toWords(expected)
	aw := toWords(actual)
	if len(ew) == 0 {
		return 1
	}
	set := map[string]bool{}
	for _, word := range aw {
		set[word] = true
	}
	match := 0
	for _, word := range ew {
		if set[word] {
			match++
		}
	}
	return float64(match) / float64(len(ew))
}

func toWords(s string) []string {
	lower := strings.ToLower(s)
	return wordRE.FindAllString(lower, -1)
}
