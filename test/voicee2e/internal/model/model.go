package model

import "time"

type RunnerConfig struct {
	GatewayURL string
	SuitePath  string
	Scenario   string
	OutPath    string
	PromptPath string
	PromptA    string
	PromptB    string
	Compare    bool
}

type SuiteSpec struct {
	Name      string         `yaml:"name"`
	Scenarios []ScenarioSpec `yaml:"scenarios"`
}

type ScenarioSpec struct {
	ID          string               `yaml:"id"`
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	TimeoutSec  int                  `yaml:"timeout_sec"`
	Audio       []AudioStep          `yaml:"audio"`
	Expect      ScenarioExpectations `yaml:"expect"`
}

type AudioStep struct {
	Fixture        string `yaml:"fixture"`
	ExpectedText   string `yaml:"expected_text"`
	DelayBeforeMS  int    `yaml:"delay_before_ms"`
	DelayAfterMS   int    `yaml:"delay_after_ms"`
	ExpectBargeIn  bool   `yaml:"expect_barge_in"`
	ExpectedIntent string `yaml:"expected_intent"`
}

type ScenarioExpectations struct {
	MinTranscriptSimilarity float64 `yaml:"min_transcript_similarity"`
	MaxResponseLatencyMS    int64   `yaml:"max_response_latency_ms"`
	MaxResponseSentences    int     `yaml:"max_response_sentences"`
	RequireBargeIn          bool    `yaml:"require_barge_in"`
	MaxBargeStopMS          int64   `yaml:"max_barge_stop_ms"`
}

type EventType string

const (
	EventSpeechStarted     EventType = "speech_started"
	EventSpeechEnded       EventType = "speech_ended"
	EventTranscriptFinal   EventType = "transcript.final"
	EventTurnCommitted     EventType = "turn_committed"
	EventTurnQueued        EventType = "turn_queued"
	EventTurnDropped       EventType = "turn_dropped"
	EventBargeInTriggered  EventType = "barge_in_triggered"
	EventResponseStarted   EventType = "response.started"
	EventResponseCompleted EventType = "response.completed"
	EventTTSInput          EventType = "tts.input"
)

type TimelineEvent struct {
	At         time.Time      `json:"at"`
	Type       EventType      `json:"type"`
	SessionID  string         `json:"session_id,omitempty"`
	TurnID     string         `json:"turn_id,omitempty"`
	ResponseID string         `json:"response_id,omitempty"`
	RunnerID   string         `json:"runner_id,omitempty"`
	Text       string         `json:"text,omitempty"`
	Value      int64          `json:"value,omitempty"`
	Raw        map[string]any `json:"raw,omitempty"`
}

type ScenarioResult struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Passed        bool            `json:"passed"`
	StartedAt     time.Time       `json:"started_at"`
	EndedAt       time.Time       `json:"ended_at"`
	DurationMS    int64           `json:"duration_ms"`
	Failures      []string        `json:"failures,omitempty"`
	Warnings      []string        `json:"warnings,omitempty"`
	Metrics       map[string]any  `json:"metrics,omitempty"`
	Timeline      []TimelineEvent `json:"timeline,omitempty"`
	PromptVariant string          `json:"prompt_variant,omitempty"`
}

type RunReport struct {
	Version       string           `json:"version"`
	SuiteName     string           `json:"suite_name"`
	GatewayURL    string           `json:"gateway_url"`
	StartedAt     time.Time        `json:"started_at"`
	EndedAt       time.Time        `json:"ended_at"`
	DurationMS    int64            `json:"duration_ms"`
	ScenarioCount int              `json:"scenario_count"`
	PassedCount   int              `json:"passed_count"`
	FailedCount   int              `json:"failed_count"`
	Results       []ScenarioResult `json:"results"`
}

type CompareReport struct {
	Version   string    `json:"version"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	BasePath  string    `json:"base_path"`
	CandPath  string    `json:"cand_path"`
	Summary   []string  `json:"summary"`
}
