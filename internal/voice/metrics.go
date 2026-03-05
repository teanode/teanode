package voice

import "sync"

// TurnMetrics captures turn lifecycle timestamps and derived latencies.
type TurnMetrics struct {
	TurnID              string `json:"turnId"`
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
	ttsRequestedMS      int64
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

func (self *MetricsObserver) OnTTSRequested(turnId string, tsMs int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	metric := self.get(turnId)
	if metric.ttsRequestedMS == 0 {
		metric.ttsRequestedMS = tsMs
	}
}

func (self *MetricsObserver) OnResponseStarted(turnId string, responseId string, tsMs int64) {
	self.mu.Lock()
	metric := self.get(turnId)
	metric.ResponseID = responseId
	metric.ResponseStartedMS = tsMs
	if metric.SpeechEndedMS > 0 && metric.TranscriptFinalMS > 0 {
		metric.STTMS = metric.TranscriptFinalMS - metric.SpeechEndedMS
	}
	if metric.TurnCommittedMS > 0 && metric.ResponseStartedMS > 0 {
		metric.LLMTTFBMS = metric.ResponseStartedMS - metric.TurnCommittedMS
	}
	if metric.ttsRequestedMS > 0 {
		metric.TTSMS = metric.ResponseStartedMS - metric.ttsRequestedMS
	}
	if metric.SpeechEndedMS > 0 && metric.ResponseStartedMS > 0 {
		metric.E2EMS = metric.ResponseStartedMS - metric.SpeechEndedMS
	}
	final := *metric
	delete(self.turns, turnId)
	self.mu.Unlock()

	if self.emit != nil {
		self.emit(final)
	}
}

func (self *MetricsObserver) OnResponseCompleted(turnId string, responseId string, tsMs int64) {
	self.mu.Lock()
	metric := self.get(turnId)
	metric.ResponseID = responseId
	metric.ResponseCompletedMS = tsMs
	self.mu.Unlock()
}

func (self *MetricsObserver) OnTurnDropped(turnId string, _ string, _ int64) {
	self.mu.Lock()
	defer self.mu.Unlock()
	delete(self.turns, turnId)
}
