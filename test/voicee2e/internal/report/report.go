package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
)

func WriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func RenderMarkdown(run *model.RunReport) string {
	var b strings.Builder
	b.WriteString("# Voice E2E Report\n\n")
	b.WriteString(fmt.Sprintf("- Suite: `%s`\n", run.SuiteName))
	b.WriteString(fmt.Sprintf("- Gateway: `%s`\n", run.GatewayURL))
	b.WriteString(fmt.Sprintf("- Scenarios: %d\n", run.ScenarioCount))
	b.WriteString(fmt.Sprintf("- Passed: %d\n", run.PassedCount))
	b.WriteString(fmt.Sprintf("- Failed: %d\n\n", run.FailedCount))
	for _, result := range run.Results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		b.WriteString(fmt.Sprintf("## %s (%s)\n\n", result.Name, status))
		if len(result.Failures) > 0 {
			b.WriteString("Failures:\n")
			for _, failure := range result.Failures {
				b.WriteString(fmt.Sprintf("- %s\n", failure))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
