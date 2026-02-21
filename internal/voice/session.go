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
	sendJSONFn   func(any)
	sendBinaryFn func([]byte)

	closeOnce   sync.Once
	bargeInOnce sync.Once
	wg          sync.WaitGroup

	stateMu            sync.RWMutex
	currentTurnID      string
	currentRunID       string
	currentResponseID  string
	lastCommittedText  string
	runCancel          func()
	ttsCancel          func()
	transcribeInFlight map[string]struct{}
	committedTurns     map[string]struct{}
	pendingTurns       []PendingTurn
	maxPendingTurns    int

	outSeq atomic.Uint64
	inSeq  atomic.Uint64

	audioInCh  chan []byte
	ttsInCh    chan string
	audioOutCh chan []byte
	doneCh     chan struct{}
}

const (
	defaultAudioInBufferFrames  = 64
	defaultTTSSentenceBuffer    = 32
	defaultAudioOutBufferFrames = 128
	defaultMaxPendingTurns      = 3
)

// NewSession creates a session with default channel capacities.
func NewSession(id, conversationID, agentID, promptSuffix string, in, out AudioFormat, features Features, deps GatewayDeps, sendJSON func(any), sendBinary func([]byte)) *Session {
	return &Session{
		ID:                 id,
		ConversationID:     conversationID,
		AgentID:            agentID,
		PromptSuffix:       promptSuffix,
		AudioIn:            in,
		AudioOut:           out,
		Features:           features,
		deps:               deps,
		sendJSONFn:         sendJSON,
		sendBinaryFn:       sendBinary,
		transcribeInFlight: make(map[string]struct{}),
		committedTurns:     make(map[string]struct{}),
		maxPendingTurns:    defaultMaxPendingTurns,
		audioInCh:          make(chan []byte, defaultAudioInBufferFrames),
		ttsInCh:            make(chan string, defaultTTSSentenceBuffer),
		audioOutCh:         make(chan []byte, defaultAudioOutBufferFrames),
		doneCh:             make(chan struct{}),
	}
}

// Start begins session background loops.
func (s *Session) Start() {
	pipelineLog.Infof("voice session start: session=%s conv=%s agent=%s", s.ID, s.ConversationID, s.AgentID)
	s.wg.Add(4)
	go func() { defer s.wg.Done(); s.audioInputLoop() }()
	go func() { defer s.wg.Done(); s.llmEventForwarder() }()
	go func() { defer s.wg.Done(); s.ttsSynthLoop() }()
	go func() { defer s.wg.Done(); s.audioOutputLoop() }()
}

// Close stops the session and waits for loop termination.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		pipelineLog.Infof("voice session close: session=%s", s.ID)
		if prev := s.SwapRunCancel(nil); prev != nil {
			prev()
		}
		if prev := s.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		close(s.doneCh)
		s.wg.Wait()
	})
}

func (s *Session) NextOutSeq() uint64 {
	return s.outSeq.Add(1)
}

func (s *Session) enqueueAudioOut(data []byte) bool {
	select {
	case s.audioOutCh <- data:
		return true
	default:
		pipelineLog.Warningf("voice audioOut queue full: session=%s dropped_bytes=%d", s.ID, len(data))
		return false
	}
}

func (s *Session) enqueueAudioIn(data []byte) bool {
	select {
	case s.audioInCh <- data:
		return true
	default:
		pipelineLog.Warningf("voice audioIn queue full: session=%s dropped_bytes=%d", s.ID, len(data))
		return false
	}
}

// HandleInputBinaryFrame parses a client binary frame and enqueues audio payload.
func (s *Session) HandleInputBinaryFrame(raw []byte) error {
	frame, err := ParseBinaryAudioFrame(raw)
	if err != nil {
		return err
	}
	if frame.FrameType != FrameTypeAudioIn {
		return errors.New("expected audio_in frame")
	}
	s.inSeq.Store(frame.Seq)
	if frame.Seq%100 == 0 {
		pipelineLog.Debugf("voice input frame: session=%s seq=%d payload_bytes=%d", s.ID, frame.Seq, len(frame.Data))
	}
	if !s.enqueueAudioIn(frame.Data) {
		return errors.New("audio input queue full")
	}
	return nil
}

// InputCommit allows push-to-talk sessions to flush buffered input turn.
func (s *Session) InputCommit() {
	s.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: s.GetCurrentTurnID(),
		Event:  "turn_committed",
	})
}

// CancelResponse aborts current response generation and playback.
func (s *Session) CancelResponse() {
	pipelineLog.Infof("voice cancel response: session=%s response=%s run=%s", s.ID, s.GetCurrentResponseID(), s.GetCurrentRunID())
	s.triggerBargeIn()
}

func (s *Session) sendVoiceEvent(eventType string, payload interface{}) {
	if s.sendJSONFn == nil {
		return
	}
	s.sendJSONFn(map[string]interface{}{
		"v":          1,
		"type":       eventType,
		"session_id": s.ID,
		"seq":        s.NextOutSeq(),
		"ts_ms":      time.Now().UnixMilli(),
		"payload":    payload,
	})
}

func (s *Session) GetCurrentTurnID() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.currentTurnID
}

func (s *Session) SetCurrentTurnID(id string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.currentTurnID = id
}

func (s *Session) GetCurrentRunID() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.currentRunID
}

func (s *Session) SetCurrentRunID(id string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.currentRunID = id
}

func (s *Session) ClearCurrentRun() {
	s.SetCurrentRunID("")
}

func (s *Session) GetCurrentResponseID() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.currentResponseID
}

func (s *Session) SetCurrentResponseID(id string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.currentResponseID = id
}

func (s *Session) ClearCurrentResponse() {
	s.SetCurrentResponseID("")
}

func (s *Session) GetLastCommittedTranscript() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.lastCommittedText
}

func (s *Session) SetLastCommittedTranscript(text string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.lastCommittedText = text
}

func (s *Session) SwapRunCancel(cancelFn func()) (prev func()) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	prev = s.runCancel
	s.runCancel = cancelFn
	return prev
}

func (s *Session) SwapTTSCancel(cancelFn func()) (prev func()) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	prev = s.ttsCancel
	s.ttsCancel = cancelFn
	return prev
}

func (s *Session) newTurnID() string {
	return security.NewULID()
}

func (s *Session) TryStartTurnTranscription(turnID string) bool {
	if turnID == "" {
		return false
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if _, exists := s.committedTurns[turnID]; exists {
		return false
	}
	if _, exists := s.transcribeInFlight[turnID]; exists {
		return false
	}
	s.transcribeInFlight[turnID] = struct{}{}
	return true
}

func (s *Session) FinishTurnTranscription(turnID string) {
	if turnID == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	delete(s.transcribeInFlight, turnID)
}

func (s *Session) HasTranscriptionInFlight() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return len(s.transcribeInFlight) > 0
}

func (s *Session) IsTurnCommitted(turnID string) bool {
	if turnID == "" {
		return false
	}
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	_, exists := s.committedTurns[turnID]
	return exists
}

func (s *Session) MarkTurnCommitted(turnID string) {
	if turnID == "" {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.committedTurns[turnID] = struct{}{}
}

func (s *Session) EnqueuePendingTurn(turnID, text string) (dropped *PendingTurn, queueDepth int) {
	if turnID == "" || text == "" {
		return nil, 0
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	maxPending := s.maxPendingTurns
	if maxPending <= 0 {
		maxPending = defaultMaxPendingTurns
	}
	if len(s.pendingTurns) >= maxPending {
		oldest := s.pendingTurns[0]
		s.pendingTurns = s.pendingTurns[1:]
		dropped = &oldest
	}
	s.pendingTurns = append(s.pendingTurns, PendingTurn{
		TurnID:    turnID,
		Text:      text,
		CreatedAt: time.Now(),
	})
	return dropped, len(s.pendingTurns)
}

func (s *Session) DequeuePendingTurn() (PendingTurn, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if len(s.pendingTurns) == 0 {
		return PendingTurn{}, false
	}
	next := s.pendingTurns[0]
	s.pendingTurns = s.pendingTurns[1:]
	return next, true
}

func (s *Session) HasPendingTurns() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return len(s.pendingTurns) > 0
}

func (s *Session) DropOldestPendingTurn(_ string) (PendingTurn, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if len(s.pendingTurns) == 0 {
		return PendingTurn{}, false
	}
	dropped := s.pendingTurns[0]
	s.pendingTurns = s.pendingTurns[1:]
	return dropped, true
}
