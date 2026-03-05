package model

import "time"

type RunnerConfiguration struct {
	GatewayURL string
	SuitePath  string
	Scenario   string
	OutputPath string
	ConfigJSON string
	PromptPath string
	PromptA    string
	PromptB    string
	Compare    bool
}

type SuiteSpecification struct {
	Name      string                  `yaml:"name"`
	Scenarios []ScenarioSpecification `yaml:"scenarios"`
}

type ScenarioSpecification struct {
	ID             string               `yaml:"id"`
	Name           string               `yaml:"name"`
	Description    string               `yaml:"description"`
	TimeoutSeconds int                  `yaml:"timeout_sec"`
	Audio          []AudioStep          `yaml:"audio"`
	Expect         ScenarioExpectations `yaml:"expect"`
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
	EventBargeInTriggered  EventType = "bargeInTriggered"
	EventResponseStarted   EventType = "response.started"
	EventResponseCompleted EventType = "response.completed"
	EventTTSInput          EventType = "tts.input"
	EventTurnMetrics       EventType = "turn.metrics"
)

type TurnMetrics struct {
	TurnID              string `json:"turnId,omitempty"`
	ResponseID          string `json:"responseId,omitempty"`
	SpeechStartedMS     int64  `json:"speechStartedMs,omitempty"`
	SpeechEndedMS       int64  `json:"speechEndedMs,omitempty"`
	TranscriptFinalMS   int64  `json:"transcriptFinalMs,omitempty"`
	TurnCommittedMS     int64  `json:"turnCommittedMs,omitempty"`
	ResponseStartedMS   int64  `json:"responseStartedMs,omitempty"`
	ResponseCompletedMS int64  `json:"responseCompletedMs,omitempty"`
	STTMS               int64  `json:"sttMs,omitempty"`
	LLMTTFBMS           int64  `json:"llmTtfbMs,omitempty"`
	TTSMS               int64  `json:"ttsMs,omitempty"`
	E2EMS               int64  `json:"e2eMs,omitempty"`
}

type TimelineEvent struct {
	At         time.Time      `json:"at"`
	Type       EventType      `json:"type"`
	SessionID  string         `json:"sessionId,omitempty"`
	TurnID     string         `json:"turnId,omitempty"`
	ResponseID string         `json:"responseId,omitempty"`
	RunID      string         `json:"runId,omitempty"`
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
	TurnMetrics   []TurnMetrics   `json:"turn_metrics,omitempty"`
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
	Version       string    `json:"version"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	BasePath      string    `json:"base_path"`
	CandidatePath string    `json:"cand_path"`
	Summary       []string  `json:"summary"`
}
