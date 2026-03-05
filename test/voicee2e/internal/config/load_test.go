package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSuite(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "suite.yaml")
	content := `
name: test-suite
scenarios:
  - id: s1
    name: scenario one
    audio:
      - fixture: one.wav
    expect:
      min_transcript_similarity: 0.5
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}
	suite, err := LoadSuite(path)
	if err != nil {
		t.Fatalf("load suite: %v", err)
	}
	if suite.Name != "test-suite" || len(suite.Scenarios) != 1 {
		t.Fatalf("unexpected suite: %#v", suite)
	}
}

func TestFilterScenario(t *testing.T) {
	t.Parallel()
	suite, err := LoadSuite(filepath.Join("..", "..", "scenarios", "suite.yaml"))
	if err != nil {
		t.Fatalf("load built-in suite: %v", err)
	}
	filtered, err := FilterScenario(suite, "s1_short")
	if err != nil {
		t.Fatalf("filter scenario: %v", err)
	}
	if len(filtered.Scenarios) != 1 || filtered.Scenarios[0].ID != "s1_short" {
		t.Fatalf("unexpected filtered result: %#v", filtered)
	}
}
