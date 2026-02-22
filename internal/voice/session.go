package voice

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teanode/teanode/internal/util/security"
)

// AudioFormat defines negotiated audio transport settings.
type AudioFormat struct {
	Codec        string `json:"codec"`
	SampleRateHz int    `json:"sample_rate_hz"`
	Channels     int    `json:"channels"`
	FrameMS      int    `json:"frame_ms,omitempty"`
}

// Features defines enabled voice pipeline features.
type Features struct {
	ServerVAD     bool `json:"server_vad"`
	ServerTurn    bool `json:"server_turn"`
	ServerDenoise bool `json:"server_denoise"`
	BargeIn       bool `json:"barge_in"`
}

type turnEventPayload struct {
	TurnID      string  `json:"turn_id,omitempty"`
	Event       string  `json:"event"`
	Reason      string  `json:"reason,omitempty"`
	QueueDepth  int     `json:"queue_depth,omitempty"`
	VADScore    float64 `json:"vad_score,omitempty"`
	AudioSeqRef uint64  `json:"audio_seq_ref,omitempty"`
}

type PendingTurn struct {
	TurnID    string
	Text      string
	CreatedAt time.Time
}

// Session owns the lifecycle and concurrency state for one voice connection.
type Session struct {
	ID             string
	ConversationID string
	AgentID        string
	PromptSuffix   string
	AudioIn        AudioFormat
	AudioOut       AudioFormat
	Features       Features

	deps         GatewayDeps
	sendJsonFn   func(any)
	sendBinaryFn func([]byte)

	closeOnce   sync.Once
	bargeInOnce sync.Once
	wg          sync.WaitGroup

	stateMu            sync.RWMutex
	currentTurnId      string
	currentRunId       string
	currentResponseId  string
	lastCommittedText  string
	runCancel          func()
	ttsCancel          func()
	transcribeInFlight map[string]struct{}
	committedTurns     map[string]struct{}
	canceledRuns       map[string]struct{}
	pendingTurns       []PendingTurn
	maxPendingTurns    int
	explicitAudioBuf   []byte
	speechReady        bool

	outSeq atomic.Uint64
	inSeq  atomic.Uint64

	audioInCh  chan []byte
	ttsInCh    chan string
	audioOutCh chan []byte
	doneCh     chan struct{}
}

const (
	defaultAudioInBufferFrames  = 64
	defaultTtsSentenceBuffer    = 32
	defaultAudioOutBufferFrames = 128
	defaultMaxPendingTurns      = 3
)

// NewSession creates a session with default channel capacities.
func NewSession(id, conversationId, agentId, promptSuffix string, in, out AudioFormat, features Features, deps GatewayDeps, sendJson func(any), sendBinary func([]byte)) *Session {
	return &Session{
		ID:                 id,
		ConversationID:     conversationId,
		AgentID:            agentId,
		PromptSuffix:       promptSuffix,
		AudioIn:            in,
		AudioOut:           out,
		Features:           features,
		deps:               deps,
		sendJsonFn:         sendJson,
		sendBinaryFn:       sendBinary,
		transcribeInFlight: make(map[string]struct{}),
		committedTurns:     make(map[string]struct{}),
		canceledRuns:       make(map[string]struct{}),
		maxPendingTurns:    defaultMaxPendingTurns,
		audioInCh:          make(chan []byte, defaultAudioInBufferFrames),
		ttsInCh:            make(chan string, defaultTtsSentenceBuffer),
		audioOutCh:         make(chan []byte, defaultAudioOutBufferFrames),
		doneCh:             make(chan struct{}),
	}
}

// Start begins session background loops.
func (self *Session) Start() {
	pipelineLog.Infof("voice session start: session=%s conv=%s agent=%s", self.ID, self.ConversationID, self.AgentID)
	self.wg.Add(4)
	go func() { defer self.wg.Done(); self.audioInputLoop() }()
	go func() { defer self.wg.Done(); self.llmEventForwarder() }()
	go func() { defer self.wg.Done(); self.ttsSynthLoop() }()
	go func() { defer self.wg.Done(); self.audioOutputLoop() }()
}

// Close stops the session and waits for loop termination.
func (self *Session) Close() {
	self.closeOnce.Do(func() {
		pipelineLog.Infof("voice session close: session=%s", self.ID)
		if prev := self.SwapRunCancel(nil); prev != nil {
			prev()
		}
		if prev := self.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		close(self.doneCh)
		self.wg.Wait()
	})
}

func (self *Session) NextOutSeq() uint64 {
	return self.outSeq.Add(1)
}

func (self *Session) enqueueAudioOut(data []byte) bool {
	select {
	case self.audioOutCh <- data:
		return true
	default:
		pipelineLog.Warningf("voice audioOut queue full: session=%s dropped_bytes=%d", self.ID, len(data))
		return false
	}
}

func (self *Session) enqueueAudioIn(data []byte) bool {
	select {
	case self.audioInCh <- data:
		return true
	default:
		pipelineLog.Warningf("voice audioIn queue full: session=%s dropped_bytes=%d", self.ID, len(data))
		return false
	}
}

// HandleInputBinaryFrame parses a client binary frame and enqueues audio payload.
func (self *Session) HandleInputBinaryFrame(raw []byte) error {
	frame, err := ParseBinaryAudioFrame(raw)
	if err != nil {
		return err
	}
	if frame.FrameType != FrameTypeAudioIn {
		return errors.New("expected audio_in frame")
	}
	self.inSeq.Store(frame.Seq)
	if frame.Seq%100 == 0 {
		pipelineLog.Debugf("voice input frame: session=%s seq=%d payload_bytes=%d", self.ID, frame.Seq, len(frame.Data))
	}
	if !self.enqueueAudioIn(frame.Data) {
		return errors.New("audio input queue full")
	}
	return nil
}

// InputCommit allows push-to-talk sessions to flush buffered input turn.
func (self *Session) InputCommit(reason string) {
	turnId := self.GetCurrentTurnId()
	if turnId == "" {
		turnId = self.newTurnId()
		self.startNewTurn(turnId)
	}
	audio := self.takeExplicitAudio()
	if len(audio) == 0 {
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turn_dropped",
			Reason: "dropped_empty_audio",
		})
		return
	}
	if len(audio) < minCommittedTurnBytes {
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turn_dropped",
			Reason: "dropped_too_short_audio",
		})
		return
	}

	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnId,
		Event:  "input_committed",
		Reason: reason,
	})
	self.setSpeechReady(false)
	if !self.TryStartTurnTranscription(turnId) {
		return
	}
	go func(tid string, captured []byte) {
		defer self.FinishTurnTranscription(tid)
		self.transcribeAndSend(tid, captured)
	}(turnId, audio)
}

// CancelResponse aborts current response generation and playback.
func (self *Session) CancelResponse() {
	pipelineLog.Infof("voice cancel response: session=%s response=%s run=%s", self.ID, self.GetCurrentResponseId(), self.GetCurrentRunId())
	self.triggerBargeIn()
}

func (self *Session) sendVoiceEvent(eventType string, payload interface{}) {
	if self.sendJsonFn == nil {
		return
	}
	self.sendJsonFn(map[string]interface{}{
		"v":          1,
		"type":       eventType,
		"session_id": self.ID,
		"seq":        self.NextOutSeq(),
		"ts_ms":      time.Now().UnixMilli(),
		"payload":    payload,
	})
}

func (self *Session) GetCurrentTurnId() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.currentTurnId
}

func (self *Session) SetCurrentTurnId(id string) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.currentTurnId = id
}

func (self *Session) GetCurrentRunId() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.currentRunId
}

func (self *Session) SetCurrentRunId(id string) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.currentRunId = id
}

func (self *Session) ClearCurrentRun() {
	self.SetCurrentRunId("")
}

func (self *Session) GetCurrentResponseId() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.currentResponseId
}

func (self *Session) SetCurrentResponseId(id string) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.currentResponseId = id
}

func (self *Session) ClearCurrentResponse() {
	self.SetCurrentResponseId("")
}

func (self *Session) GetLastCommittedTranscript() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.lastCommittedText
}

func (self *Session) SetLastCommittedTranscript(text string) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.lastCommittedText = text
}

func (self *Session) SwapRunCancel(cancelFn func()) (prev func()) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	prev = self.runCancel
	self.runCancel = cancelFn
	return prev
}

func (self *Session) SwapTTSCancel(cancelFn func()) (prev func()) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	prev = self.ttsCancel
	self.ttsCancel = cancelFn
	return prev
}

func (self *Session) newTurnId() string {
	return security.NewULID()
}

func (self *Session) TryStartTurnTranscription(turnId string) bool {
	if turnId == "" {
		return false
	}
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	if _, exists := self.committedTurns[turnId]; exists {
		return false
	}
	if _, exists := self.transcribeInFlight[turnId]; exists {
		return false
	}
	self.transcribeInFlight[turnId] = struct{}{}
	return true
}

func (self *Session) FinishTurnTranscription(turnId string) {
	if turnId == "" {
		return
	}
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	delete(self.transcribeInFlight, turnId)
}

func (self *Session) HasTranscriptionInFlight() bool {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return len(self.transcribeInFlight) > 0
}

func (self *Session) IsTurnCommitted(turnId string) bool {
	if turnId == "" {
		return false
	}
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	_, exists := self.committedTurns[turnId]
	return exists
}

func (self *Session) MarkTurnCommitted(turnId string) {
	if turnId == "" {
		return
	}
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.committedTurns[turnId] = struct{}{}
}

func (self *Session) MarkRunCanceled(runId string) {
	if runId == "" {
		return
	}
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.canceledRuns[runId] = struct{}{}
}

func (self *Session) IsRunCanceled(runId string) bool {
	if runId == "" {
		return false
	}
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	_, exists := self.canceledRuns[runId]
	return exists
}

func (self *Session) ClearCanceledRun(runId string) {
	if runId == "" {
		return
	}
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	delete(self.canceledRuns, runId)
}

func (self *Session) EnqueuePendingTurn(turnId, text string) (dropped *PendingTurn, queueDepth int) {
	if turnId == "" || text == "" {
		return nil, 0
	}
	self.stateMu.Lock()
	defer self.stateMu.Unlock()

	maxPending := self.maxPendingTurns
	if maxPending <= 0 {
		maxPending = defaultMaxPendingTurns
	}
	if len(self.pendingTurns) >= maxPending {
		oldest := self.pendingTurns[0]
		self.pendingTurns = self.pendingTurns[1:]
		dropped = &oldest
	}
	self.pendingTurns = append(self.pendingTurns, PendingTurn{
		TurnID:    turnId,
		Text:      text,
		CreatedAt: time.Now(),
	})
	return dropped, len(self.pendingTurns)
}

func (self *Session) DequeuePendingTurn() (PendingTurn, bool) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	if len(self.pendingTurns) == 0 {
		return PendingTurn{}, false
	}
	next := self.pendingTurns[0]
	self.pendingTurns = self.pendingTurns[1:]
	return next, true
}

func (self *Session) HasPendingTurns() bool {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return len(self.pendingTurns) > 0
}

func (self *Session) DropOldestPendingTurn(_ string) (PendingTurn, bool) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	if len(self.pendingTurns) == 0 {
		return PendingTurn{}, false
	}
	dropped := self.pendingTurns[0]
	self.pendingTurns = self.pendingTurns[1:]
	return dropped, true
}

func (self *Session) accumulateExplicitAudio(frame []byte) {
	if len(frame) == 0 {
		return
	}
	self.stateMu.Lock()
	self.explicitAudioBuf = append(self.explicitAudioBuf, frame...)
	self.stateMu.Unlock()
}

func (self *Session) setSpeechReady(ready bool) {
	self.stateMu.Lock()
	self.speechReady = ready
	self.stateMu.Unlock()
}

func (self *Session) IsSpeechReady() bool {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.speechReady
}

func (self *Session) ExplicitAudioLen() int {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return len(self.explicitAudioBuf)
}

func (self *Session) takeExplicitAudio() []byte {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	if len(self.explicitAudioBuf) == 0 {
		return nil
	}
	captured := append([]byte(nil), self.explicitAudioBuf...)
	self.explicitAudioBuf = self.explicitAudioBuf[:0]
	return captured
}
