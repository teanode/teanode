package report

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func TestWriteJSONAndRenderMarkdown(t *testing.T) {
	t.Parallel()
	report := &model.RunReport{
		Version:       "v1",
		SuiteName:     "suite",
		GatewayURL:    "http://127.0.0.1:8833",
		StartedAt:     time.Now(),
		EndedAt:       time.Now(),
		ScenarioCount: 1,
		PassedCount:   1,
		Results: []model.ScenarioResult{
			{ID: "s1", Name: "Scenario 1", Passed: true},
		},
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := WriteJSON(path, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	md := RenderMarkdown(report)
	if !strings.Contains(md, "Voice E2E Report") {
		t.Fatalf("missing report header in markdown: %s", md)
	}
}
