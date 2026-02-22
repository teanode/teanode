package voice

import "sync"

// TurnMetrics captures turn lifecycle timestamps and derived latencies.
type TurnMetrics struct {
	TurnID              string `json:"turn_id"`
	ResponseID          string `json:"response_id,omitempty"`
	SpeechStartedMS     int64  `json:"speech_started_ms,omitempty"`
	SpeechEndedMS       int64  `json:"speech_ended_ms,omitempty"`
	TranscriptFinalMS   int64  `json:"transcript_final_ms,omitempty"`
	TurnCommittedMS     int64  `json:"turn_committed_ms,omitempty"`
	ResponseStartedMS   int64  `json:"response_started_ms,omitempty"`
	ResponseCompletedMS int64  `json:"response_completed_ms,omitempty"`
	STTMS               int64  `json:"stt_ms,omitempty"`
	LLMTTFBMS           int64  `json:"llm_ttfb_ms,omitempty"`
	TTSMS               int64  `json:"tts_ms,omitempty"`
	E2EMS               int64  `json:"e2e_ms,omitempty"`
}

type MetricsObserver struct {
	mu    sync.Mutex
	turns map[string]*TurnMetrics
	emit  func(TurnMetrics)
}

func NewMetricsObserver(emit func(TurnMetrics)) *MetricsObserver {
	return &MetricsObserver{
		turns: make(map[string]*TurnMetrics),
		emit:  emit,
	}
}

func (self *MetricsObserver) get(turnId string) *TurnMetrics {
	metric, ok := self.turns[turnId]
	if !ok {
		metric = &TurnMetrics{TurnID: turnId}
		self.turns[turnId] = metric
	}
	return metric
}

func (self *MetricsObserver) OnSpeechStarted(turnId string, tsMs int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.get(turnId).SpeechStartedMS = tsMs
}

func (self *MetricsObserver) OnSpeechEnded(turnId string, tsMs int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.get(turnId).SpeechEndedMS = tsMs
}

func (self *MetricsObserver) OnTranscriptFinal(turnId string, tsMs int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.get(turnId).TranscriptFinalMS = tsMs
}

func (self *MetricsObserver) OnTurnCommitted(turnId string, tsMs int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.get(turnId).TurnCommittedMS = tsMs
}

func (self *MetricsObserver) OnResponseStarted(turnId string, responseId string, tsMs int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	metric := self.get(turnId)
	metric.ResponseID = responseId
	metric.ResponseStartedMS = tsMs
}

func (self *MetricsObserver) OnResponseCompleted(turnId string, responseId string, tsMs int64) {
	self.mu.Lock()
	metric := self.get(turnId)
	metric.ResponseID = responseId
	metric.ResponseCompletedMS = tsMs
	if metric.SpeechEndedMS > 0 && metric.TranscriptFinalMS > 0 {
		metric.STTMS = metric.TranscriptFinalMS - metric.SpeechEndedMS
	}
	if metric.TurnCommittedMS > 0 && metric.ResponseStartedMS > 0 {
		metric.LLMTTFBMS = metric.ResponseStartedMS - metric.TurnCommittedMS
	}
	if metric.ResponseStartedMS > 0 && metric.ResponseCompletedMS > 0 {
		metric.TTSMS = metric.ResponseCompletedMS - metric.ResponseStartedMS
	}
	if metric.SpeechEndedMS > 0 && metric.ResponseCompletedMS > 0 {
		metric.E2EMS = metric.ResponseCompletedMS - metric.SpeechEndedMS
	}
	final := *metric
	delete(self.turns, turnId)
	self.mu.Unlock()

	if self.emit != nil {
		self.emit(final)
	}
}

func (self *MetricsObserver) OnTurnDropped(turnId string, _ string, _ int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	delete(self.turns, turnId)
}
