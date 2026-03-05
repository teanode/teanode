package voice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
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
	ServerVAD    bool   `json:"server_vad"`
	ServerTurn   bool   `json:"server_turn"`
	BargeIn      bool   `json:"barge_in"`
	TurnStrategy string `json:"turn_strategy,omitempty"`
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

	dispatcher   Dispatcher
	events       *pubsub.PubSub
	sendJsonFn   func(any)
	sendBinaryFn func([]byte)

	closeOnce       sync.Once
	wg              sync.WaitGroup
	transcriptionWg sync.WaitGroup // tracks goroutines spawned by commitCapturedTurn / InputCommit

	// Barge-in generation: bargeInGen is incremented by startNewTurn each time
	// a new turn begins. bargeInFired is set to newGen-1 by startNewTurn,
	// establishing the precondition for triggerBargeIn's CAS(gen-1 → gen).
	// That CAS succeeds exactly once per generation even under concurrent
	// callers, eliminating the sync.Once reset race that existed when
	// startNewTurn wrote bargeInOnce = sync.Once{} under stateMu.
	// Invariant: bargeInFired == bargeInGen-1 means "not yet fired this turn".
	// bargeInGen starts at 1; bargeInFired starts at 0, so the first turn's
	// CAS(0 → 1) works without an explicit startNewTurn call.
	bargeInGen   atomic.Uint64
	bargeInFired atomic.Uint64

	stateMu sync.RWMutex // guards all fields below

	// Turn lifecycle: identity and commit tracking.
	currentTurnId      string
	pendingTurns       []PendingTurn
	maxPendingTurns    int
	committedTurns     map[string]struct{}
	lastCommittedText  string
	transcribeInFlight map[string]struct{}

	// Run/response lifecycle: active run and TTS cancellation.
	currentRunId          string
	currentResponseId     string
	currentResponseTurnId string
	runCancel             func()
	ttsCancel             func()
	canceledRuns          map[string]struct{}
	runTurn               map[string]string

	// Speech boundary: VAD and barge-in state.
	speechReady     bool
	speechStartedAt time.Time
	userSpeaking    bool
	userSpeakingCh  chan struct{} // open while speaking; closed/nil when silent
	lastBargeInAt   time.Time
	strategy        TurnStrategy

	// Streaming STT: live transcription stream and interim results.
	explicitAudioBuf     []byte
	streamingSTTStream   providers.TranscribeStream
	interimText          string
	interimBestText      string
	streamingFinalTurnID string
	streamingFinalText   string

	// Observers: registered turn lifecycle listeners.
	observers []TurnObserver

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
func NewSession(id, conversationId, agentId, promptSuffix string, in, out AudioFormat, features Features, dispatcher Dispatcher, events *pubsub.PubSub, sendJson func(any), sendBinary func([]byte)) *Session {
	session := &Session{
		ID:                 id,
		ConversationID:     conversationId,
		AgentID:            agentId,
		PromptSuffix:       promptSuffix,
		AudioIn:            in,
		AudioOut:           out,
		Features:           features,
		dispatcher:         dispatcher,
		events:             events,
		sendJsonFn:         sendJson,
		sendBinaryFn:       sendBinary,
		transcribeInFlight: make(map[string]struct{}),
		committedTurns:     make(map[string]struct{}),
		canceledRuns:       make(map[string]struct{}),
		runTurn:            make(map[string]string),
		maxPendingTurns:    defaultMaxPendingTurns,
		audioInCh:          make(chan []byte, defaultAudioInBufferFrames),
		ttsInCh:            make(chan string, defaultTtsSentenceBuffer),
		audioOutCh:         make(chan []byte, defaultAudioOutBufferFrames),
		doneCh:             make(chan struct{}),
		strategy:           LegacyStrategy{},
	}
	if strings.EqualFold(features.TurnStrategy, "balanced") {
		session.strategy = BalancedStrategy{}
	}
	// bargeInGen starts at 1; bargeInFired starts at 0 (zero value).
	// triggerBargeIn fires for gen N via CAS(N-1 → N) on bargeInFired, so
	// gen=1 fires when fired transitions from 0 to 1.
	session.bargeInGen.Store(1)
	session.observers = []TurnObserver{
		NewMetricsObserver(func(metric TurnMetrics) {
			session.sendVoiceEvent("turn.metrics", metric)
		}),
	}
	return session
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
		if stream := self.getStreamingTranscribeStream(); stream != nil {
			_ = stream.Close()
		}
		close(self.doneCh)
		self.wg.Wait()
		self.transcriptionWg.Wait()
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
		self.notifyObservers(func(observer TurnObserver) {
			observer.OnTurnDropped(turnId, "dropped_empty_audio", time.Now().UnixMilli())
		})
		return
	}
	if len(audio) < minCommittedTurnBytes {
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turn_dropped",
			Reason: "dropped_too_short_audio",
		})
		self.notifyObservers(func(observer TurnObserver) {
			observer.OnTurnDropped(turnId, "dropped_too_short_audio", time.Now().UnixMilli())
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
	self.transcriptionWg.Add(1)
	go func(tid string, captured []byte) {
		defer self.transcriptionWg.Done()
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

func (self *Session) MapRunToTurn(runId, turnId string) {
	if runId == "" || turnId == "" {
		return
	}
	self.stateMu.Lock()
	self.runTurn[runId] = turnId
	self.stateMu.Unlock()
}

func (self *Session) TurnIDForRun(runId string) string {
	if runId == "" {
		return ""
	}
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.runTurn[runId]
}

func (self *Session) ClearRunTurn(runId string) {
	if runId == "" {
		return
	}
	self.stateMu.Lock()
	delete(self.runTurn, runId)
	self.stateMu.Unlock()
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
	if id == "" {
		self.currentResponseTurnId = ""
	}
}

func (self *Session) ClearCurrentResponse() {
	self.SetCurrentResponseId("")
}

func (self *Session) GetCurrentResponseTurnId() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.currentResponseTurnId
}

func (self *Session) SetCurrentResponseTurnId(turnId string) {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	self.currentResponseTurnId = turnId
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

func (self *Session) DropOldestPendingTurn() (PendingTurn, bool) {
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

func (self *Session) startStreamingTranscriber() bool {
	if self.dispatcher == nil || self.dispatcher.ProviderRegistry() == nil {
		return false
	}
	streaming, provider, ok := self.dispatcher.ProviderRegistry().FindStreamingTranscriber()
	if !ok || streaming == nil {
		return false
	}
	stream, err := streaming.OpenTranscribeStream(context.Background(), providers.StreamTranscribeRequest{
		SampleRate: self.AudioIn.SampleRateHz,
		Channels:   self.AudioIn.Channels,
		Prompt:     self.transcriptionPrompt(),
	})
	if err != nil {
		pipelineLog.Warningf("voice streaming stt open failed, falling back to batch: provider=%s err=%v", provider, err)
		return false
	}
	self.stateMu.Lock()
	self.streamingSTTStream = stream
	self.stateMu.Unlock()
	pipelineLog.Infof("voice streaming stt enabled: session=%s provider=%s model=%s", self.ID, provider, voiceProviderModelHint("streaming_transcriber", provider))
	return true
}

func (self *Session) getStreamingTranscribeStream() providers.TranscribeStream {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.streamingSTTStream
}

func (self *Session) setStreamingTranscribeStream(stream providers.TranscribeStream) {
	self.stateMu.Lock()
	self.streamingSTTStream = stream
	self.stateMu.Unlock()
}

func (self *Session) setInterimText(text string) {
	self.stateMu.Lock()
	self.interimText = text
	if len([]rune(strings.TrimSpace(text))) >= len([]rune(strings.TrimSpace(self.interimBestText))) {
		self.interimBestText = text
	}
	self.stateMu.Unlock()
}

func (self *Session) getInterimText() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.interimText
}

func (self *Session) getBestInterimText() string {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	if strings.TrimSpace(self.interimBestText) != "" {
		return self.interimBestText
	}
	return self.interimText
}

func (self *Session) setStreamingFinalText(turnId, text string) {
	self.stateMu.Lock()
	self.streamingFinalTurnID = turnId
	self.streamingFinalText = text
	self.stateMu.Unlock()
}

func (self *Session) takeStreamingFinalText(turnId string) string {
	self.stateMu.Lock()
	defer self.stateMu.Unlock()
	if self.streamingFinalTurnID != turnId {
		return ""
	}
	text := self.streamingFinalText
	self.streamingFinalTurnID = ""
	self.streamingFinalText = ""
	return text
}

func (self *Session) setSpeechStartedAt(ts time.Time) {
	self.stateMu.Lock()
	self.speechStartedAt = ts
	self.stateMu.Unlock()
}

func (self *Session) setLastBargeInAt(ts time.Time) {
	self.stateMu.Lock()
	self.lastBargeInAt = ts
	self.stateMu.Unlock()
}

func (self *Session) recentBargeInWithin(window time.Duration) bool {
	self.stateMu.RLock()
	last := self.lastBargeInAt
	self.stateMu.RUnlock()
	if last.IsZero() {
		return false
	}
	return time.Since(last) < window
}

func (self *Session) speechDurationMs(now time.Time) int {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	if self.speechStartedAt.IsZero() {
		return 0
	}
	return int(now.Sub(self.speechStartedAt).Milliseconds())
}

func (self *Session) setUserSpeaking(speaking bool) {
	self.stateMu.Lock()
	self.userSpeaking = speaking
	if speaking {
		// Create a new open channel; ttsSynthLoop blocks on it.
		self.userSpeakingCh = make(chan struct{})
	} else if self.userSpeakingCh != nil {
		// Close the channel to unblock any waiter; ttsSynthLoop select wakes up.
		close(self.userSpeakingCh)
		self.userSpeakingCh = nil
	}
	self.stateMu.Unlock()
}

func (self *Session) IsUserSpeaking() bool {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.userSpeaking
}

// getUserSpeakingCh returns the current speaking channel under the read lock.
// An open channel means the user is speaking (caller should wait); a nil channel
// means the user is silent (caller can proceed).
func (self *Session) getUserSpeakingCh() chan struct{} {
	self.stateMu.RLock()
	defer self.stateMu.RUnlock()
	return self.userSpeakingCh
}

// RunIsActive reports whether an LLM run is currently in progress.
// Use this instead of comparing GetCurrentRunId() to "" directly.
func (self *Session) RunIsActive() bool {
	return self.GetCurrentRunId() != ""
}

// ResponseIsActive reports whether a TTS response is currently being produced.
// Use this instead of comparing GetCurrentResponseId() to "" directly.
func (self *Session) ResponseIsActive() bool {
	return self.GetCurrentResponseId() != ""
}

// BargeInIsArmed reports whether a barge-in interruption should fire: either a
// run or a response is active, meaning new speech should cancel the current output.
func (self *Session) BargeInIsArmed() bool {
	return self.RunIsActive() || self.ResponseIsActive()
}

func (self *Session) notifyObservers(fn func(observer TurnObserver)) {
	if len(self.observers) == 0 {
		return
	}
	for _, observer := range self.observers {
		if observer == nil {
			continue
		}
		fn(observer)
	}
}
