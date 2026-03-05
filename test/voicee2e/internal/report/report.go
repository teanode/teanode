package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func WriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func RenderMarkdown(run *model.RunReport) string {
	var buffer strings.Builder
	buffer.WriteString("# Voice E2E Report\n\n")
	buffer.WriteString(fmt.Sprintf("- Suite: `%s`\n", run.SuiteName))
	buffer.WriteString(fmt.Sprintf("- Gateway: `%s`\n", run.GatewayURL))
	buffer.WriteString(fmt.Sprintf("- Scenarios: %d\n", run.ScenarioCount))
	buffer.WriteString(fmt.Sprintf("- Passed: %d\n", run.PassedCount))
	buffer.WriteString(fmt.Sprintf("- Failed: %d\n\n", run.FailedCount))

	latencySummary := summarizeLatencies(run)
	if latencySummary != "" {
		buffer.WriteString("## Latency Percentiles\n\n")
		buffer.WriteString(latencySummary)
		buffer.WriteString("\n")
	}

	for _, result := range run.Results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		buffer.WriteString(fmt.Sprintf("## %s (%s)\n\n", result.Name, status))
		if len(result.Failures) > 0 {
			buffer.WriteString("Failures:\n")
			for _, failure := range result.Failures {
				buffer.WriteString(fmt.Sprintf("- %s\n", failure))
			}
			buffer.WriteString("\n")
		}
	}
	return buffer.String()
}

func summarizeLatencies(run *model.RunReport) string {
	var e2e []int64
	var stt []int64
	var llm []int64
	var tts []int64
	for _, scenario := range run.Results {
		for _, metric := range scenario.TurnMetrics {
			if metric.E2EMS > 0 {
				e2e = append(e2e, metric.E2EMS)
			}
			if metric.STTMS > 0 {
				stt = append(stt, metric.STTMS)
			}
			if metric.LLMTTFBMS > 0 {
				llm = append(llm, metric.LLMTTFBMS)
			}
			if metric.TTSMS > 0 {
				tts = append(tts, metric.TTSMS)
			}
		}
	}
	if len(e2e)+len(stt)+len(llm)+len(tts) == 0 {
		return ""
	}

	var b strings.Builder
	writeLatencyLine := func(name string, values []int64) {
		if len(values) == 0 {
			return
		}
		p50 := percentile(values, 50)
		p90 := percentile(values, 90)
		p99 := percentile(values, 99)
		b.WriteString(fmt.Sprintf("- %s: p50=%dms p90=%dms p99=%dms\n", name, p50, p90, p99))
	}
	writeLatencyLine("e2e_ms", e2e)
	writeLatencyLine("stt_ms", stt)
	writeLatencyLine("llm_ttfb_ms", llm)
	writeLatencyLine("tts_ms", tts)
	return b.String()
}

func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (len(sorted) - 1) * p / 100
	return sorted[index]
}
