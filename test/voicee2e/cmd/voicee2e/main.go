package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/teanode/teanode/test/voicee2e/internal/model"
	"github.com/teanode/teanode/test/voicee2e/internal/report"
	"github.com/teanode/teanode/test/voicee2e/internal/runner"
)

func main() {
	var cfg model.RunnerConfig
	flag.StringVar(&cfg.GatewayURL, "gateway-url", "http://127.0.0.1:8833", "TeaNode gateway base URL")
	flag.StringVar(&cfg.SuitePath, "suite", "test/voicee2e/scenarios/suite.yaml", "Path to suite YAML")
	flag.StringVar(&cfg.Scenario, "scenario", "", "Run only one scenario ID")
	flag.StringVar(&cfg.OutPath, "out", "", "Output JSON report path")
	flag.StringVar(&cfg.PromptPath, "prompt", "", "Prompt variant file path")
	flag.BoolVar(&cfg.Compare, "compare", false, "Compare mode")
	flag.StringVar(&cfg.PromptA, "prompt-a", "", "Prompt A report JSON path")
	flag.StringVar(&cfg.PromptB, "prompt-b", "", "Prompt B report JSON path")
	flag.Parse()

	if cfg.OutPath == "" {
		cfg.OutPath = filepath.Join("test", "voicee2e", "reports", fmt.Sprintf("report-%d.json", time.Now().Unix()))
	}

	if cfg.Compare {
		if err := compareReports(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "compare failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()
	run := runner.New(cfg.GatewayURL)
	result, err := run.RunSuite(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(1)
	}
	if err := report.WriteJSON(cfg.OutPath, result); err != nil {
		fmt.Fprintf(os.Stderr, "write report failed: %v\n", err)
		os.Exit(1)
	}
	mdPath := cfg.OutPath + ".md"
	if err := os.WriteFile(mdPath, []byte(report.RenderMarkdown(result)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write markdown report failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("voicee2e report: %s\n", cfg.OutPath)
	if result.FailedCount > 0 {
		os.Exit(2)
	}
}

func compareReports(cfg model.RunnerConfig) error {
	if cfg.PromptA == "" || cfg.PromptB == "" {
		return fmt.Errorf("--compare requires --prompt-a and --prompt-b")
	}
	var base model.RunReport
	var cand model.RunReport
	if err := readJson(cfg.PromptA, &base); err != nil {
		return fmt.Errorf("read prompt-a: %w", err)
	}
	if err := readJson(cfg.PromptB, &cand); err != nil {
		return fmt.Errorf("read prompt-b: %w", err)
	}
	start := time.Now()
	summary := []string{
		fmt.Sprintf("base failed=%d/%d", base.FailedCount, base.ScenarioCount),
		fmt.Sprintf("candidate failed=%d/%d", cand.FailedCount, cand.ScenarioCount),
	}
	comp := model.CompareReport{
		Version:   "v1",
		StartedAt: start,
		EndedAt:   time.Now(),
		BasePath:  cfg.PromptA,
		CandPath:  cfg.PromptB,
		Summary:   summary,
	}
	out := cfg.OutPath
	if out == "" {
		out = filepath.Join("test", "voicee2e", "reports", fmt.Sprintf("compare-%d.json", time.Now().Unix()))
	}
	return report.WriteJSON(out, comp)
}

func readJson(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
