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
	"github.com/teanode/teanode/internal/util/deferutil"
	"github.com/teanode/teanode/internal/util/security"
)

// AudioFormat defines negotiated audio transport settings.
type AudioFormat struct {
	Codec        string `json:"codec"`
	SampleRateHz int    `json:"sampleRateHz"`
	Channels     int    `json:"channels"`
	FrameMS      int    `json:"frameMs,omitempty"`
}

// Features defines enabled voice pipeline features.
type Features struct {
	ServerVAD    bool   `json:"serverVad"`
	ServerTurn   bool   `json:"serverTurn"`
	BargeIn      bool   `json:"bargeIn"`
	TurnStrategy string `json:"turnStrategy,omitempty"`
}

type turnEventPayload struct {
	TurnID      string  `json:"turnId,omitempty"`
	Event       string  `json:"event"`
	Reason      string  `json:"reason,omitempty"`
	QueueDepth  int     `json:"queueDepth,omitempty"`
	VADScore    float64 `json:"vadScore,omitempty"`
	AudioSeqRef uint64  `json:"audioSeqRef,omitempty"`
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
	AudioIn        AudioFormat
	AudioOut       AudioFormat
	Features       Features

	dispatcher         Dispatcher
	events             *pubsub.PubSub
	sendJsonFunction   func(any)
	sendBinaryFunction func([]byte)

	closeOnce              sync.Once
	waitGroup              sync.WaitGroup
	transcriptionWaitGroup sync.WaitGroup // tracks goroutines spawned by commitCapturedTurn / InputCommit

	// Barge-in generation: bargeInGen is incremented by startNewTurn each time
	// a new turn begins. bargeInFired is set to newGen-1 by startNewTurn,
	// establishing the precondition for triggerBargeIn's CAS(gen-1 → gen).
	// That CAS succeeds exactly once per generation even under concurrent
	// callers, eliminating the sync.Once reset race that existed when
	// startNewTurn wrote bargeInOnce = sync.Once{} under stateMutex.
	// Invariant: bargeInFired == bargeInGen-1 means "not yet fired this turn".
	// bargeInGen starts at 1; bargeInFired starts at 0, so the first turn's
	// CAS(0 → 1) works without an explicit startNewTurn call.
	bargeInGen   atomic.Uint64
	bargeInFired atomic.Uint64

	stateMutex sync.RWMutex // guards all fields below

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
	speechReady         bool
	speechStartedAt     time.Time
	userSpeaking        bool
	userSpeakingChannel chan struct{} // open while speaking; closed/nil when silent
	lastBargeInAt       time.Time
	strategy            TurnStrategy

	// Streaming STT: live transcription stream and interim results.
	explicitAudioBuffer  []byte
	streamingSttStream   providers.TranscribeStream
	interimText          string
	interimBestText      string
	streamingFinalTurnId string
	streamingFinalText   string

	// Observers: registered turn lifecycle listeners.
	observers []TurnObserver

	outSeq atomic.Uint64
	inSeq  atomic.Uint64

	audioInChannel  chan []byte
	ttsInChannel    chan string
	audioOutChannel chan []byte
	doneChannel     chan struct{}
}

const (
	defaultAudioInBufferFrames  = 64
	defaultTtsSentenceBuffer    = 32
	defaultAudioOutBufferFrames = 128
	defaultMaxPendingTurns      = 3
)

// NewSession creates a session with default channel capacities.
func NewSession(id, conversationId, agentId string, in, out AudioFormat, features Features, dispatcher Dispatcher, events *pubsub.PubSub, sendJson func(any), sendBinary func([]byte)) *Session {
	session := &Session{
		ID:                 id,
		ConversationID:     conversationId,
		AgentID:            agentId,
		AudioIn:            in,
		AudioOut:           out,
		Features:           features,
		dispatcher:         dispatcher,
		events:             events,
		sendJsonFunction:   sendJson,
		sendBinaryFunction: sendBinary,
		transcribeInFlight: make(map[string]struct{}),
		committedTurns:     make(map[string]struct{}),
		canceledRuns:       make(map[string]struct{}),
		runTurn:            make(map[string]string),
		maxPendingTurns:    defaultMaxPendingTurns,
		audioInChannel:     make(chan []byte, defaultAudioInBufferFrames),
		ttsInChannel:       make(chan string, defaultTtsSentenceBuffer),
		audioOutChannel:    make(chan []byte, defaultAudioOutBufferFrames),
		doneChannel:        make(chan struct{}),
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
		close(self.doneChannel)
		self.waitGroup.Wait()
		self.transcriptionWaitGroup.Wait()
	})
}

func (self *Session) NextOutSeq() uint64 {
	return self.outSeq.Add(1)
}

func (self *Session) enqueueAudioOut(data []byte) bool {
	select {
	case self.audioOutChannel <- data:
		return true
	default:
		pipelineLog.Warningf("voice audioOut queue full: session=%s dropped_bytes=%d", self.ID, len(data))
		return false
	}
}

func (self *Session) enqueueAudioIn(data []byte) bool {
	select {
	case self.audioInChannel <- data:
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
		return errors.New("voice: expected audio_in frame")
	}
	self.inSeq.Store(frame.Seq)
	if frame.Seq%100 == 0 {
		pipelineLog.Debugf("voice input frame: session=%s seq=%d payload_bytes=%d", self.ID, frame.Seq, len(frame.Data))
	}
	if !self.enqueueAudioIn(frame.Data) {
		return errors.New("voice: audio input queue full")
	}
	return nil
}

// InputCommit allows push-to-talk sessions to flush buffered input turn.
func (self *Session) InputCommit(reason string) {
	turnId := self.GetCurrentTurnID()
	if turnId == "" {
		turnId = self.newTurnId()
		self.startNewTurn(turnId)
	}
	audio := self.takeExplicitAudio()
	if len(audio) == 0 {
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turnDropped",
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
			Event:  "turnDropped",
			Reason: "droppedTooShortAudio",
		})
		self.notifyObservers(func(observer TurnObserver) {
			observer.OnTurnDropped(turnId, "droppedTooShortAudio", time.Now().UnixMilli())
		})
		return
	}

	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnId,
		Event:  "inputCommitted",
		Reason: reason,
	})
	self.setSpeechReady(false)
	if !self.TryStartTurnTranscription(turnId) {
		return
	}
	self.transcriptionWaitGroup.Add(1)
	go func(tid string, captured []byte) {
		defer deferutil.Recover()
		defer self.transcriptionWaitGroup.Done()
		defer self.FinishTurnTranscription(tid)
		self.transcribeAndSend(tid, captured)
	}(turnId, audio)
}

// CancelResponse aborts current response generation and playback.
func (self *Session) CancelResponse() {
	pipelineLog.Infof("voice cancel response: session=%s response=%s run=%s", self.ID, self.GetCurrentResponseID(), self.GetCurrentRunID())
	self.triggerBargeIn()
}

func (self *Session) sendVoiceEvent(eventType string, payload interface{}) {
	if self.sendJsonFunction == nil {
		return
	}
	self.sendJsonFunction(map[string]interface{}{
		"v":         1,
		"type":      eventType,
		"sessionId": self.ID,
		"seq":       self.NextOutSeq(),
		"tsMs":      time.Now().UnixMilli(),
		"payload":   payload,
	})
}

func (self *Session) GetCurrentTurnID() string {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.currentTurnId
}

func (self *Session) SetCurrentTurnID(id string) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.currentTurnId = id
}

func (self *Session) GetCurrentRunID() string {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.currentRunId
}

func (self *Session) SetCurrentRunID(id string) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.currentRunId = id
}

func (self *Session) ClearCurrentRun() {
	self.SetCurrentRunID("")
}

func (self *Session) MapRunToTurn(runId, turnId string) {
	if runId == "" || turnId == "" {
		return
	}
	self.stateMutex.Lock()
	self.runTurn[runId] = turnId
	self.stateMutex.Unlock()
}

func (self *Session) TurnIDForRun(runId string) string {
	if runId == "" {
		return ""
	}
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.runTurn[runId]
}

func (self *Session) ClearRunTurn(runId string) {
	if runId == "" {
		return
	}
	self.stateMutex.Lock()
	delete(self.runTurn, runId)
	self.stateMutex.Unlock()
}

func (self *Session) GetCurrentResponseID() string {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.currentResponseId
}

func (self *Session) SetCurrentResponseID(id string) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.currentResponseId = id
	if id == "" {
		self.currentResponseTurnId = ""
	}
}

func (self *Session) ClearCurrentResponse() {
	self.SetCurrentResponseID("")
}

func (self *Session) GetCurrentResponseTurnID() string {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.currentResponseTurnId
}

func (self *Session) SetCurrentResponseTurnID(turnId string) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.currentResponseTurnId = turnId
}

func (self *Session) GetLastCommittedTranscript() string {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.lastCommittedText
}

func (self *Session) SetLastCommittedTranscript(text string) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.lastCommittedText = text
}

func (self *Session) SwapRunCancel(cancelFunction func()) (prev func()) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	prev = self.runCancel
	self.runCancel = cancelFunction
	return prev
}

func (self *Session) SwapTTSCancel(cancelFunction func()) (prev func()) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	prev = self.ttsCancel
	self.ttsCancel = cancelFunction
	return prev
}

func (self *Session) newTurnId() string {
	return security.NewULID()
}

func (self *Session) TryStartTurnTranscription(turnId string) bool {
	if turnId == "" {
		return false
	}
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
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
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	delete(self.transcribeInFlight, turnId)
}

func (self *Session) HasTranscriptionInFlight() bool {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return len(self.transcribeInFlight) > 0
}

func (self *Session) IsTurnCommitted(turnId string) bool {
	if turnId == "" {
		return false
	}
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	_, exists := self.committedTurns[turnId]
	return exists
}

func (self *Session) MarkTurnCommitted(turnId string) {
	if turnId == "" {
		return
	}
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.committedTurns[turnId] = struct{}{}
}

func (self *Session) MarkRunCanceled(runId string) {
	if runId == "" {
		return
	}
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	self.canceledRuns[runId] = struct{}{}
}

func (self *Session) IsRunCanceled(runId string) bool {
	if runId == "" {
		return false
	}
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	_, exists := self.canceledRuns[runId]
	return exists
}

func (self *Session) ClearCanceledRun(runId string) {
	if runId == "" {
		return
	}
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	delete(self.canceledRuns, runId)
}

func (self *Session) EnqueuePendingTurn(turnId, text string) (dropped *PendingTurn, queueDepth int) {
	if turnId == "" || text == "" {
		return nil, 0
	}
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()

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
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	if len(self.pendingTurns) == 0 {
		return PendingTurn{}, false
	}
	next := self.pendingTurns[0]
	self.pendingTurns = self.pendingTurns[1:]
	return next, true
}

func (self *Session) HasPendingTurns() bool {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return len(self.pendingTurns) > 0
}

func (self *Session) DropOldestPendingTurn() (PendingTurn, bool) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
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
	self.stateMutex.Lock()
	self.explicitAudioBuffer = append(self.explicitAudioBuffer, frame...)
	self.stateMutex.Unlock()
}

func (self *Session) setSpeechReady(ready bool) {
	self.stateMutex.Lock()
	self.speechReady = ready
	self.stateMutex.Unlock()
}

func (self *Session) IsSpeechReady() bool {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.speechReady
}

func (self *Session) ExplicitAudioLength() int {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return len(self.explicitAudioBuffer)
}

func (self *Session) takeExplicitAudio() []byte {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	if len(self.explicitAudioBuffer) == 0 {
		return nil
	}
	captured := append([]byte(nil), self.explicitAudioBuffer...)
	self.explicitAudioBuffer = self.explicitAudioBuffer[:0]
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
	stream, err := streaming.TranscribeStream(context.Background(), providers.StreamTranscribeRequest{
		SampleRate: self.AudioIn.SampleRateHz,
		Channels:   self.AudioIn.Channels,
		Prompt:     self.transcriptionPrompt(),
	})
	if err != nil {
		pipelineLog.Warningf("voice streaming stt open failed, falling back to batch: provider=%s err=%v", provider, err)
		return false
	}
	self.stateMutex.Lock()
	self.streamingSttStream = stream
	self.stateMutex.Unlock()
	pipelineLog.Infof("voice streaming stt enabled: session=%s provider=%s model=%s", self.ID, provider, voiceProviderModelHint("streaming_transcriber", provider))
	return true
}

func (self *Session) getStreamingTranscribeStream() providers.TranscribeStream {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.streamingSttStream
}

func (self *Session) setStreamingTranscribeStream(stream providers.TranscribeStream) {
	self.stateMutex.Lock()
	self.streamingSttStream = stream
	self.stateMutex.Unlock()
}

func (self *Session) setInterimText(text string) {
	self.stateMutex.Lock()
	self.interimText = text
	if len([]rune(strings.TrimSpace(text))) >= len([]rune(strings.TrimSpace(self.interimBestText))) {
		self.interimBestText = text
	}
	self.stateMutex.Unlock()
}

func (self *Session) getInterimText() string {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.interimText
}

func (self *Session) setStreamingFinalText(turnId, text string) {
	self.stateMutex.Lock()
	self.streamingFinalTurnId = turnId
	self.streamingFinalText = text
	self.stateMutex.Unlock()
}

func (self *Session) takeStreamingFinalText(turnId string) string {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	if self.streamingFinalTurnId != turnId {
		return ""
	}
	text := self.streamingFinalText
	self.streamingFinalTurnId = ""
	self.streamingFinalText = ""
	return text
}

func (self *Session) setSpeechStartedAt(ts time.Time) {
	self.stateMutex.Lock()
	self.speechStartedAt = ts
	self.stateMutex.Unlock()
}

func (self *Session) setLastBargeInAt(ts time.Time) {
	self.stateMutex.Lock()
	self.lastBargeInAt = ts
	self.stateMutex.Unlock()
}

func (self *Session) speechDurationMs(now time.Time) int {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	if self.speechStartedAt.IsZero() {
		return 0
	}
	return int(now.Sub(self.speechStartedAt).Milliseconds())
}

func (self *Session) setUserSpeaking(speaking bool) {
	self.stateMutex.Lock()
	self.userSpeaking = speaking
	if speaking {
		// Create a new open channel; ttsSynthLoop blocks on it.
		self.userSpeakingChannel = make(chan struct{})
	} else if self.userSpeakingChannel != nil {
		// Close the channel to unblock any waiter; ttsSynthLoop select wakes up.
		close(self.userSpeakingChannel)
		self.userSpeakingChannel = nil
	}
	self.stateMutex.Unlock()
}

func (self *Session) IsUserSpeaking() bool {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.userSpeaking
}

// getUserSpeakingChannel returns the current speaking channel under the read lock.
// An open channel means the user is speaking (caller should wait); a nil channel
// means the user is silent (caller can proceed).
func (self *Session) getUserSpeakingChannel() chan struct{} {
	self.stateMutex.RLock()
	defer self.stateMutex.RUnlock()
	return self.userSpeakingChannel
}

// RunIsActive reports whether an LLM run is currently in progress.
// Use this instead of comparing GetCurrentRunID() to "" directly.
func (self *Session) RunIsActive() bool {
	return self.GetCurrentRunID() != ""
}

// ResponseIsActive reports whether a TTS response is currently being produced.
// Use this instead of comparing GetCurrentResponseID() to "" directly.
func (self *Session) ResponseIsActive() bool {
	return self.GetCurrentResponseID() != ""
}

// BargeInIsArmed reports whether a barge-in interruption should fire: either a
// run or a response is active, meaning new speech should cancel the current output.
func (self *Session) BargeInIsArmed() bool {
	return self.RunIsActive() || self.ResponseIsActive()
}

func (self *Session) notifyObservers(function func(observer TurnObserver)) {
	if len(self.observers) == 0 {
		return
	}
	for _, observer := range self.observers {
		if observer == nil {
			continue
		}
		function(observer)
	}
}
