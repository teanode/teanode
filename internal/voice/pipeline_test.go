package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
)

// pipelineMockTranscriberProvider implements Provider + TranscribeProvider.
type pipelineMockTranscriberProvider struct {
	providers.BaseProvider
	mu    sync.Mutex
	text  string
	delay time.Duration
	calls int
}

func (self *pipelineMockTranscriberProvider) Transcribe(_ context.Context, request providers.TranscribeRequest) (*providers.TranscribeResponse, error) {
	self.mu.Lock()
	self.calls++
	delay := self.delay
	text := self.text
	self.mu.Unlock()
	// Drain audio reader to satisfy interface contract.
	if request.Audio != nil {
		_, _ = io.ReadAll(request.Audio)
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return &providers.TranscribeResponse{Text: text}, nil
}

func (self *pipelineMockTranscriberProvider) callCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.calls
}

// pipelineMockStreamingTranscriberProvider implements Provider + StreamingTranscribeProvider.
type pipelineMockStreamingTranscriberProvider struct {
	providers.BaseProvider
	stream providers.TranscribeStream
}

func (self *pipelineMockStreamingTranscriberProvider) TranscribeStream(context.Context, providers.StreamTranscribeRequest) (providers.TranscribeStream, error) {
	return self.stream, nil
}

// pipelineMockSynthesizerProvider implements Provider + SynthesizeProvider + StreamingSynthesizeProvider.
type pipelineMockSynthesizerProvider struct {
	providers.BaseProvider
	synthesizeFn       func(context.Context, providers.SynthesizeRequest) (*providers.SynthesizeResponse, error)
	synthesizeStreamFn func(context.Context, providers.SynthesizeStreamRequest) (<-chan providers.SynthesizeChunk, error)
}

func (self *pipelineMockSynthesizerProvider) Synthesize(ctx context.Context, request providers.SynthesizeRequest) (*providers.SynthesizeResponse, error) {
	if self.synthesizeFn != nil {
		return self.synthesizeFn(ctx, request)
	}
	return nil, fmt.Errorf("synthesize not configured")
}

func (self *pipelineMockSynthesizerProvider) SynthesizeStream(ctx context.Context, request providers.SynthesizeStreamRequest) (<-chan providers.SynthesizeChunk, error) {
	if self.synthesizeStreamFn != nil {
		return self.synthesizeStreamFn(ctx, request)
	}
	return nil, fmt.Errorf("synthesize stream not configured")
}

type pipelineMockTranscribeStream struct {
	events chan providers.TranscribeStreamEvent
}

func newPipelineMockTranscribeStream() *pipelineMockTranscribeStream {
	return &pipelineMockTranscribeStream{events: make(chan providers.TranscribeStreamEvent, 8)}
}

func (self *pipelineMockTranscribeStream) SendAudio([]byte) error { return nil }
func (self *pipelineMockTranscribeStream) Events() <-chan providers.TranscribeStreamEvent {
	return self.events
}
func (self *pipelineMockTranscribeStream) Close() error {
	close(self.events)
	return nil
}

type scriptedTurnStrategy struct {
	mu            sync.Mutex
	decisions     []TurnDecision
	commitAllowed bool
}

func (self *scriptedTurnStrategy) EvaluateBargeIn(TurnContext) TurnDecision {
	self.mu.Lock()
	defer self.mu.Unlock()
	if len(self.decisions) == 0 {
		return TurnDecisionIgnore
	}
	decision := self.decisions[0]
	self.decisions = self.decisions[1:]
	return decision
}

func (self *scriptedTurnStrategy) ShouldCommitTurn(TurnContext) bool {
	return self.commitAllowed
}

// pipelineMockDispatcher satisfies the Dispatcher interface for tests.
type pipelineMockDispatcher struct {
	mu               sync.Mutex
	runCounter       int
	sendCalls        []coordinators.RunParameters
	abortCalls       []string
	providerRegistry *providers.ProviderRegistry
}

type eventRecorder struct {
	mu     sync.Mutex
	events []map[string]interface{}
}

func (self *eventRecorder) append(value any) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	self.events = append(self.events, m)
}

func (self *eventRecorder) findTurnEvent(event string) map[string]interface{} {
	self.mu.Lock()
	defer self.mu.Unlock()
	for i := len(self.events) - 1; i >= 0; i-- {
		entry := self.events[i]
		if entry["type"] != "turn.event" {
			continue
		}
		if payload, ok := entry["payload"].(turnEventPayload); ok {
			if payload.Event == event {
				return entry
			}
			continue
		}
		if payload, ok := entry["payload"].(map[string]interface{}); ok {
			name, _ := payload["event"].(string)
			if name == event {
				return entry
			}
		}
	}
	return nil
}

func (self *pipelineMockDispatcher) Run(_ context.Context, parameters coordinators.RunParameters, _ *runners.RunCallbacks) (*coordinators.RunHandle, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.runCounter++
	self.sendCalls = append(self.sendCalls, parameters)
	handle := coordinators.NewRunHandle(fmt.Sprintf("run-%d", self.runCounter), parameters.ConversationID)
	handle.Resolve(nil, nil)
	return handle, nil
}

func (self *pipelineMockDispatcher) AbortRun(runId string) bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.abortCalls = append(self.abortCalls, runId)
	return true
}

func (self *pipelineMockDispatcher) ProviderRegistry() *providers.ProviderRegistry {
	return self.providerRegistry
}

func (self *pipelineMockDispatcher) sendCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.sendCalls)
}

func (self *pipelineMockDispatcher) lastSend() coordinators.RunParameters {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.sendCalls[len(self.sendCalls)-1]
}

func (self *pipelineMockDispatcher) abortCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.abortCalls)
}

func newMockRegistry(transcriber *pipelineMockTranscriberProvider) *providers.ProviderRegistry {
	providerRegistry := providers.NewEmptyProviderRegistry()
	if transcriber != nil {
		providerRegistry.Register("mock-stt", transcriber)
	}
	return providerRegistry
}

func newPipelineSessionWithFeatures(text string, features Features) (*Session, *pipelineMockDispatcher) {
	transcriber := &pipelineMockTranscriberProvider{text: text}
	providerRegistry := newMockRegistry(transcriber)
	dispatcher := &pipelineMockDispatcher{providerRegistry: providerRegistry}
	s := NewSession(
		"sess",
		"conv",
		"agent",
		"",
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1},
		features,
		dispatcher,
		nil,
		nil,
		nil,
	)
	return s, dispatcher
}

func newPipelineSession(text string) (*Session, *pipelineMockDispatcher) {
	return newPipelineSessionWithFeatures(text, Features{BargeIn: true, ServerVAD: true, ServerTurn: true})
}

func newPipelineSessionWithEventsAndFeatures(text string, features Features) (*Session, *pipelineMockDispatcher, *eventRecorder) {
	rec := &eventRecorder{}
	transcriber := &pipelineMockTranscriberProvider{text: text}
	providerRegistry := newMockRegistry(transcriber)
	dispatcher := &pipelineMockDispatcher{providerRegistry: providerRegistry}
	s := NewSession(
		"sess",
		"conv",
		"agent",
		"",
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1},
		features,
		dispatcher,
		nil,
		rec.append,
		nil,
	)
	return s, dispatcher, rec
}

func newPipelineSessionWithEvents(text string) (*Session, *pipelineMockDispatcher, *eventRecorder) {
	return newPipelineSessionWithEventsAndFeatures(text, Features{BargeIn: true, ServerVAD: true, ServerTurn: true})
}

func makePCMFrame(sample int16, samples int) []byte {
	buf := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:i*2+2], uint16(sample))
	}
	return buf
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestTranscribeQueuesWhenRunActive(t *testing.T) {
	s, dispatcher := newPipelineSession("hello from queued turn")
	s.SetCurrentRunID("run-active")
	s.SetCurrentResponseID("response-active")

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	waitFor(t, 500*time.Millisecond, func() bool { return dispatcher.sendCount() == 1 })

	if dispatcher.abortCount() != 1 {
		t.Fatalf("expected one abort call for barge-in when response active, got %d", dispatcher.abortCount())
	}
	if s.HasPendingTurns() {
		t.Fatal("expected queued turn to be committed immediately after barge-in")
	}
}

func TestTranscribePreemptsQueuedTurnBeforeResponseStarts(t *testing.T) {
	s, dispatcher := newPipelineSession("hello from queued turn")
	s.SetCurrentRunID("run-active")
	s.SetCurrentResponseID("response-active")

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	waitFor(t, 500*time.Millisecond, func() bool { return dispatcher.sendCount() == 1 })

	if dispatcher.abortCount() != 1 {
		t.Fatalf("expected one abort call for barge-in, got %d", dispatcher.abortCount())
	}
	if s.HasPendingTurns() {
		t.Fatal("expected queued turn to be committed immediately after preemption")
	}
	parameters := dispatcher.lastSend()
	if parameters.Message != "hello from queued turn" {
		t.Fatalf("unexpected committed message after preemption: %q", parameters.Message)
	}
}

func TestTranscribeDoesNotPreemptWhenResponseAlreadyStarted(t *testing.T) {
	s, dispatcher := newPipelineSession("hello from queued turn")
	s.SetCurrentRunID("run-active")
	s.SetCurrentResponseID("response-active")

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 1 {
		t.Fatalf("expected immediate send after barge-in when response active, got %d", dispatcher.sendCount())
	}
	if dispatcher.abortCount() != 1 {
		t.Fatalf("expected one abort call while response active, got %d", dispatcher.abortCount())
	}
	if s.HasPendingTurns() {
		t.Fatal("expected no pending turn after response-active barge-in path")
	}
}

func TestCommitNextPendingTurnAfterTerminal(t *testing.T) {
	s, dispatcher := newPipelineSession("hello from queued turn")
	s.SetCurrentRunID("run-active")
	s.EnqueuePendingTurn("turn-1", "hello from queued turn")
	if !s.HasPendingTurns() {
		t.Fatal("expected queued turn before drain")
	}

	s.ClearCurrentRun()
	s.commitNextPendingTurn()

	if dispatcher.sendCount() != 1 {
		t.Fatalf("expected exactly one send after drain, got %d", dispatcher.sendCount())
	}
	parameters := dispatcher.lastSend()
	if parameters.Message != "hello from queued turn" {
		t.Fatalf("unexpected drained message %q", parameters.Message)
	}
	if parameters.SystemPromptSuffix == "" {
		t.Fatal("expected voice system prompt suffix on committed turn")
	}
	if s.GetCurrentRunID() == "" {
		t.Fatal("expected run id set after committing drained turn")
	}
}

func TestCommitVoiceTurnIncludesPromptSuffix(t *testing.T) {
	s, dispatcher := newPipelineSession("this should commit now")

	s.transcribeAndSend("turn-commit", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 1 {
		t.Fatalf("expected one send call, got %d", dispatcher.sendCount())
	}
	parameters := dispatcher.lastSend()
	if parameters.SystemPromptSuffix == "" {
		t.Fatal("expected non-empty voice prompt suffix")
	}
}

func TestAudioInputLoopTriggersBargeInWhenRunActive(t *testing.T) {
	s, dispatcher := newPipelineSession("unused")
	s.SetCurrentRunID("run-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for i := 0; i < 10; i++ {
		s.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return dispatcher.abortCount() > 0 && s.GetCurrentRunID() == ""
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestTranscribeEmptyTextEmitsDroppedReason(t *testing.T) {
	s, dispatcher, rec := newPipelineSessionWithEvents("   ")
	s.transcribeAndSend("turn-empty", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no send for empty transcript, got %d", dispatcher.sendCount())
	}
	ev := rec.findTurnEvent("turnDropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "droppedEmptyTranscript" {
		t.Fatalf("expected dropped_empty_transcript reason, got %q", payload.Reason)
	}
}

func TestQueueOverflowDropsOldestWithReason(t *testing.T) {
	s, dispatcher, rec := newPipelineSessionWithEvents("queued transcript text")
	s.maxPendingTurns = 1
	s.SetCurrentRunID("run-active")
	s.SetCurrentResponseID("response-active")

	s.enqueueTranscriptTurn("turn-1", "queued transcript text")
	s.enqueueTranscriptTurn("turn-2", "queued transcript text")

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no sends while run active, got %d", dispatcher.sendCount())
	}
	ev := rec.findTurnEvent("turnDropped")
	if ev == nil {
		t.Fatal("expected overflow turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "droppedQueueOverflow" {
		t.Fatalf("expected dropped_queue_overflow reason, got %q", payload.Reason)
	}
}

func TestAudioInputLoopTriggersBargeInWhenResponseActive(t *testing.T) {
	s, dispatcher := newPipelineSession("unused")
	s.SetCurrentResponseID("response-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for i := 0; i < 10; i++ {
		s.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return dispatcher.abortCount() == 0 && s.GetCurrentResponseID() == ""
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestBargeInCandidate_EventEmitted(t *testing.T) {
	s, _, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.strategy = &scriptedTurnStrategy{
		decisions:     []TurnDecision{TurnDecisionCandidate},
		commitAllowed: true,
	}
	s.SetCurrentRunID("run-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	for i := 0; i < 20; i++ {
		s.audioInCh <- makePCMFrame(12000, 320)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.findTurnEvent("bargeInCandidate") != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.findTurnEvent("bargeInCandidate") == nil {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		t.Fatalf("expected barge_in_candidate event, got events: %#v", rec.events)
	}

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestBargeInSuppressed_EventEmitted(t *testing.T) {
	s, _, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.strategy = &scriptedTurnStrategy{
		decisions:     []TurnDecision{TurnDecisionCandidate, TurnDecisionIgnore},
		commitAllowed: true,
	}
	s.SetCurrentRunID("run-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	for i := 0; i < 20; i++ {
		s.audioInCh <- makePCMFrame(12000, 320)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if rec.findTurnEvent("bargeInSuppressed") != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.findTurnEvent("bargeInSuppressed") == nil {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		t.Fatalf("expected barge_in_suppressed event, got events: %#v", rec.events)
	}

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestAudioInputLoop_ServerVADFalse(t *testing.T) {
	s, _, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for i := 0; i < 100; i++ {
		s.audioInCh <- loud
	}

	time.Sleep(100 * time.Millisecond)
	if event := rec.findTurnEvent("speechStarted"); event != nil {
		t.Fatal("speech_started should not be emitted when ServerVAD=false")
	}
	if event := rec.findTurnEvent("speechEnded"); event != nil {
		t.Fatal("speech_ended should not be emitted when ServerVAD=false")
	}
	if s.ExplicitAudioLen() == 0 {
		t.Fatal("expected explicit audio buffer to accumulate when ServerVAD=false")
	}

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestAudioInputLoop_ServerTurnFalse(t *testing.T) {
	s, dispatcher, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: false,
		BargeIn:    true,
	})

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	quiet := makePCMFrame(0, 320)
	for i := 0; i < 12; i++ {
		s.audioInCh <- loud
	}
	for i := 0; i < vadRedemptionFrames+5; i++ {
		s.audioInCh <- quiet
	}

	waitFor(t, 500*time.Millisecond, func() bool { return s.IsSpeechReady() })
	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no automatic SendMessage when ServerTurn=false, got %d", dispatcher.sendCount())
	}
	if event := rec.findTurnEvent("turnCommitted"); event != nil {
		t.Fatal("turn_committed should not be emitted when ServerTurn=false")
	}

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestInputCommit_FullPipeline(t *testing.T) {
	s, dispatcher, rec := newPipelineSessionWithEventsAndFeatures("hello from commit", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.accumulateExplicitAudio(makePCMFrame(12000, 8000)) // 500 ms

	s.InputCommit("push_to_talk")
	waitFor(t, 500*time.Millisecond, func() bool { return dispatcher.sendCount() == 1 })

	if rec.findTurnEvent("inputCommitted") == nil {
		t.Fatal("expected input_committed event")
	}
	if dispatcher.sendCount() != 1 {
		t.Fatalf("expected one send after input commit, got %d", dispatcher.sendCount())
	}
}

func TestInputCommit_EmptyBuffer(t *testing.T) {
	s, dispatcher, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})

	s.InputCommit("push_to_talk")
	time.Sleep(50 * time.Millisecond)

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no send for empty commit, got %d", dispatcher.sendCount())
	}
	ev := rec.findTurnEvent("turnDropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "dropped_empty_audio" {
		t.Fatalf("expected dropped_empty_audio reason, got %q", payload.Reason)
	}
}

func TestInputCommit_TooShort(t *testing.T) {
	s, dispatcher, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.accumulateExplicitAudio(makePCMFrame(12000, 1600)) // 100 ms

	s.InputCommit("push_to_talk")
	time.Sleep(50 * time.Millisecond)

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no send for short commit, got %d", dispatcher.sendCount())
	}
	ev := rec.findTurnEvent("turnDropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "droppedTooShortAudio" {
		t.Fatalf("expected dropped_too_short_audio reason, got %q", payload.Reason)
	}
}

func TestInputCommit_RaceCondition(t *testing.T) {
	s, dispatcher, _ := newPipelineSessionWithEventsAndFeatures("race commit", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.accumulateExplicitAudio(makePCMFrame(12000, 8000))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.InputCommit("push_to_talk")
	}()
	go func() {
		defer wg.Done()
		s.InputCommit("push_to_talk")
	}()
	wg.Wait()

	waitFor(t, 500*time.Millisecond, func() bool { return dispatcher.sendCount() >= 1 || s.HasPendingTurns() })
	if dispatcher.sendCount() > 1 {
		t.Fatalf("expected at most one send after concurrent commits, got %d", dispatcher.sendCount())
	}
}

func TestCommitCapturedTurn_PrefersStreamingFinalText(t *testing.T) {
	s, dispatcher := newPipelineSession("batch transcript should not be used")
	// Get the mock transcriber to verify it's not called.
	mockTranscriber := getMockTranscriber(t, dispatcher)
	s.setStreamingTranscribeStream(newPipelineMockTranscribeStream())
	s.setStreamingFinalText("turn-stream", "I want to learn about GitHub specifically worktree.")

	s.commitCapturedTurn("turn-stream", makePCMFrame(12000, 8000))
	waitFor(t, 800*time.Millisecond, func() bool { return dispatcher.sendCount() == 1 })

	if dispatcher.lastSend().Message != "I want to learn about GitHub specifically worktree." {
		t.Fatalf("unexpected committed transcript: %q", dispatcher.lastSend().Message)
	}
	if mockTranscriber.callCount() != 0 {
		t.Fatalf("expected no batch transcription call when streaming final is strong, got %d", mockTranscriber.callCount())
	}
}

func TestCommitCapturedTurn_FallsBackToBatchWhenStreamingFinalTooShort(t *testing.T) {
	s, dispatcher := newPipelineSession("I am planning a trip to Cheesecake Factory tell me the menu")
	mockTranscriber := getMockTranscriber(t, dispatcher)
	s.setStreamingTranscribeStream(newPipelineMockTranscribeStream())
	s.setStreamingFinalText("turn-short", "trip")

	s.commitCapturedTurn("turn-short", makePCMFrame(12000, 8000))
	waitFor(t, 1200*time.Millisecond, func() bool { return dispatcher.sendCount() == 1 })

	if dispatcher.lastSend().Message != "I am planning a trip to Cheesecake Factory tell me the menu" {
		t.Fatalf("unexpected committed transcript: %q", dispatcher.lastSend().Message)
	}
	if mockTranscriber.callCount() == 0 {
		t.Fatal("expected batch fallback transcription for short streaming final")
	}
}

func TestStreamingTranscribeLoop_FallbackOnError(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, dispatcher, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registerStreamingTranscriber(t, dispatcher, stream)
	if !s.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber to start")
	}

	streamingDone := make(chan struct{})
	go func() {
		s.streamingTranscribeLoop()
		close(streamingDone)
	}()

	audioDone := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(audioDone)
	}()

	loud := makePCMFrame(12000, 320)
	silence := makePCMFrame(0, 320)
	for i := 0; i < 12; i++ {
		s.audioInCh <- loud
	}
	stream.events <- providers.TranscribeStreamEvent{Err: context.DeadlineExceeded}
	for i := 0; i < 40; i++ {
		s.audioInCh <- silence
	}

	waitFor(t, 2*time.Second, func() bool { return dispatcher.sendCount() == 1 })
	if dispatcher.lastSend().Message != "fallback transcript" {
		t.Fatalf("unexpected fallback transcript: %q", dispatcher.lastSend().Message)
	}

	close(s.doneCh)
	select {
	case <-audioDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
	select {
	case <-streamingDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("streamingTranscribeLoop did not stop after done")
	}
}

func TestTTSSynthLoop_StreamingPath(t *testing.T) {
	s, dispatcher := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registerSynthesizer(dispatcher, &pipelineMockSynthesizerProvider{
		synthesizeStreamFn: func(_ context.Context, _ providers.SynthesizeStreamRequest) (<-chan providers.SynthesizeChunk, error) {
			out := make(chan providers.SynthesizeChunk, 2)
			out <- providers.SynthesizeChunk{Audio: []byte{1, 2}}
			out <- providers.SynthesizeChunk{Audio: []byte{3}}
			close(out)
			return out, nil
		},
	})

	done := make(chan struct{})
	go func() {
		s.ttsSynthLoop()
		close(done)
	}()

	s.ttsInCh <- "hello"
	waitFor(t, time.Second, func() bool { return len(s.audioOutCh) >= 2 })

	frame1 := <-s.audioOutCh
	frame2 := <-s.audioOutCh
	parsed1, err := ParseBinaryAudioFrame(frame1)
	if err != nil || len(parsed1.Data) != 2 {
		t.Fatalf("unexpected first audio frame: err=%v len=%d", err, len(parsed1.Data))
	}
	parsed2, err := ParseBinaryAudioFrame(frame2)
	if err != nil || len(parsed2.Data) != 1 {
		t.Fatalf("unexpected second audio frame: err=%v len=%d", err, len(parsed2.Data))
	}

	close(s.doneCh)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ttsSynthLoop did not stop after done")
	}
}

func TestTTSSynthLoop_BatchFallback(t *testing.T) {
	s, dispatcher := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	pcmData := []byte{9, 8, 7, 6}
	wavData := PCMToWAV(pcmData, 24000, 1)
	registerSynthesizer(dispatcher, &pipelineMockSynthesizerProvider{
		synthesizeFn: func(_ context.Context, _ providers.SynthesizeRequest) (*providers.SynthesizeResponse, error) {
			return &providers.SynthesizeResponse{
				Audio: io.NopCloser(bytes.NewReader(wavData)),
			}, nil
		},
	})

	done := make(chan struct{})
	go func() {
		s.ttsSynthLoop()
		close(done)
	}()

	s.ttsInCh <- "hello"
	waitFor(t, time.Second, func() bool { return len(s.audioOutCh) >= 1 })
	frame := <-s.audioOutCh
	parsed, err := ParseBinaryAudioFrame(frame)
	if err != nil {
		t.Fatalf("parse frame: %v", err)
	}
	if len(parsed.Data) != len(pcmData) {
		t.Fatalf("expected %d batch fallback bytes, got %d", len(pcmData), len(parsed.Data))
	}

	close(s.doneCh)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ttsSynthLoop did not stop after done")
	}
}

func TestTTSSynthLoop_WaitsWhileUserSpeaking(t *testing.T) {
	s, dispatcher := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registerSynthesizer(dispatcher, &pipelineMockSynthesizerProvider{
		synthesizeStreamFn: func(_ context.Context, _ providers.SynthesizeStreamRequest) (<-chan providers.SynthesizeChunk, error) {
			out := make(chan providers.SynthesizeChunk, 1)
			out <- providers.SynthesizeChunk{Audio: []byte{1, 2, 3}}
			close(out)
			return out, nil
		},
	})

	done := make(chan struct{})
	go func() {
		s.ttsSynthLoop()
		close(done)
	}()

	s.setUserSpeaking(true)
	s.ttsInCh <- "wait until speech ended"
	time.Sleep(100 * time.Millisecond)
	if len(s.audioOutCh) != 0 {
		t.Fatal("expected no TTS audio while user is speaking")
	}

	s.setUserSpeaking(false)
	waitFor(t, time.Second, func() bool { return len(s.audioOutCh) > 0 })

	close(s.doneCh)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ttsSynthLoop did not stop after done")
	}
}

func TestBargeIn_CancelsTTSStream(t *testing.T) {
	s, dispatcher := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})

	started := make(chan struct{})
	streamDone := make(chan struct{})
	registerSynthesizer(dispatcher, &pipelineMockSynthesizerProvider{
		synthesizeStreamFn: func(ctx context.Context, _ providers.SynthesizeStreamRequest) (<-chan providers.SynthesizeChunk, error) {
			out := make(chan providers.SynthesizeChunk)
			go func() {
				close(started)
				defer close(streamDone)
				<-ctx.Done()
				close(out)
			}()
			return out, nil
		},
	})

	loopDone := make(chan struct{})
	go func() {
		s.ttsSynthLoop()
		close(loopDone)
	}()

	s.SetCurrentResponseID("resp-active")
	s.ttsInCh <- "streaming sentence"
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	s.triggerBargeIn()
	select {
	case <-streamDone:
	case <-time.After(time.Second):
		t.Fatal("stream goroutine did not exit after barge-in")
	}

	close(s.doneCh)
	select {
	case <-loopDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ttsSynthLoop did not stop after done")
	}
}

func TestAudioInputLoop_StreamingNoInterimFallsBackToBatch(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, dispatcher, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registerStreamingTranscriber(t, dispatcher, stream)
	if !s.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber to start")
	}

	audioDone := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(audioDone)
	}()

	loud := makePCMFrame(12000, 320)
	silence := makePCMFrame(0, 320)
	for i := 0; i < 12; i++ {
		s.audioInCh <- loud
	}
	for i := 0; i < vadRedemptionFrames+5; i++ {
		s.audioInCh <- silence
	}

	waitFor(t, 2*time.Second, func() bool { return dispatcher.sendCount() == 1 })
	if dispatcher.lastSend().Message != "fallback transcript" {
		t.Fatalf("unexpected fallback transcript: %q", dispatcher.lastSend().Message)
	}

	close(s.doneCh)
	select {
	case <-audioDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestAudioInputLoop_StreamingFallbackTranscriptionIsNonBlocking(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, dispatcher, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	mockTranscriber := getMockTranscriber(t, dispatcher)
	mockTranscriber.delay = 250 * time.Millisecond
	registerStreamingTranscriber(t, dispatcher, stream)
	if !s.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber to start")
	}

	// Shrink the input queue so blocking behavior is easy to detect.
	s.audioInCh = make(chan []byte, 1)

	audioDone := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(audioDone)
	}()

	// First utterance triggers fallback transcription after end-of-speech.
	loud := makePCMFrame(12000, 320)
	silence := makePCMFrame(0, 320)
	for i := 0; i < 12; i++ {
		s.audioInCh <- loud
	}
	for i := 0; i < vadRedemptionFrames+5; i++ {
		s.audioInCh <- silence
	}

	// Wait until fallback transcription has started, then ensure the audio loop
	// keeps draining input while transcription runs in the background.
	waitFor(t, time.Second, func() bool { return mockTranscriber.callCount() >= 1 })
	blockedSends := 0
	deadline := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case s.audioInCh <- loud:
		case <-time.After(20 * time.Millisecond):
			blockedSends++
		}
		time.Sleep(5 * time.Millisecond)
	}
	if blockedSends > 0 {
		t.Fatalf("expected audio input to remain drainable while fallback transcription runs, got %d blocked sends", blockedSends)
	}

	waitFor(t, 2*time.Second, func() bool { return dispatcher.sendCount() == 1 })

	close(s.doneCh)
	select {
	case <-audioDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestAudioInputLoop_StreamingInterimFallsBackToBatch(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, dispatcher, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registerStreamingTranscriber(t, dispatcher, stream)
	if !s.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber to start")
	}

	streamingDone := make(chan struct{})
	go func() {
		s.streamingTranscribeLoop()
		close(streamingDone)
	}()

	audioDone := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(audioDone)
	}()

	loud := makePCMFrame(12000, 320)
	silence := makePCMFrame(0, 320)
	for i := 0; i < 12; i++ {
		s.audioInCh <- loud
	}
	stream.events <- providers.TranscribeStreamEvent{
		Type:       "interim",
		Text:       "interim transcript should not bypass batch",
		Confidence: 0.95,
	}
	for i := 0; i < vadRedemptionFrames+5; i++ {
		s.audioInCh <- silence
	}

	waitFor(t, 2*time.Second, func() bool { return dispatcher.sendCount() == 1 })
	if dispatcher.lastSend().Message != "fallback transcript" {
		t.Fatalf("unexpected transcript source: %q", dispatcher.lastSend().Message)
	}

	close(s.doneCh)
	select {
	case <-audioDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
	_ = stream.Close()
	select {
	case <-streamingDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("streamingTranscribeLoop did not stop after done")
	}
}

func TestStreamingTranscribeLoop_TracksSpeechFinalAndUtteranceEnd(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	session, dispatcher, _ := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registerStreamingTranscriber(t, dispatcher, stream)
	if !session.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber to start")
	}
	turnId := "turn-boundary"
	session.SetCurrentTurnID(turnId)

	done := make(chan struct{})
	go func() {
		session.streamingTranscribeLoop()
		close(done)
	}()

	stream.events <- providers.TranscribeStreamEvent{Type: "final", Text: "this is complete"}
	waitFor(t, 500*time.Millisecond, func() bool {
		return session.takeStreamingFinalText(turnId) != "" || session.getInterimText() == "this is complete"
	})

	close(session.doneCh)
	_ = stream.Close()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("streamingTranscribeLoop did not stop after done")
	}
}

func TestBalancedStrategy_TriggersBargeInOnSpeechStart(t *testing.T) {
	s, dispatcher := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:    true,
		ServerTurn:   true,
		BargeIn:      true,
		TurnStrategy: "balanced",
	})
	s.SetCurrentRunID("run-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	for i := 0; i < 12; i++ {
		s.audioInCh <- makePCMFrame(12000, 320)
	}
	waitFor(t, 500*time.Millisecond, func() bool {
		return dispatcher.abortCount() > 0 && s.GetCurrentRunID() == ""
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestStartNewTurn_ClearsInterimText(t *testing.T) {
	s, _ := newPipelineSession("unused")
	s.setInterimText("stale interim")
	s.startNewTurn("turn-new")
	if got := s.getInterimText(); got != "" {
		t.Fatalf("expected interim text reset on new turn, got %q", got)
	}
}

// Test helper: get the mock transcriber from the dispatcher's registry.
func getMockTranscriber(t *testing.T, dispatcher *pipelineMockDispatcher) *pipelineMockTranscriberProvider {
	t.Helper()
	transcriber, _, ok := dispatcher.providerRegistry.FindTranscriber()
	if !ok {
		t.Fatal("expected transcriber in registry")
	}
	mock, ok := transcriber.(*pipelineMockTranscriberProvider)
	if !ok {
		t.Fatal("expected pipelineMockTranscriberProvider")
	}
	return mock
}

// registerStreamingTranscriber adds a streaming transcriber to the dispatcher's registry.
func registerStreamingTranscriber(t *testing.T, dispatcher *pipelineMockDispatcher, stream *pipelineMockTranscribeStream) {
	t.Helper()
	dispatcher.providerRegistry.Register("mock-streaming-stt", &pipelineMockStreamingTranscriberProvider{stream: stream})
}

// registerSynthesizer adds a synthesizer to the dispatcher's registry.
func registerSynthesizer(dispatcher *pipelineMockDispatcher, synth *pipelineMockSynthesizerProvider) {
	dispatcher.providerRegistry.Register("mock-tts", synth)
}

// infiniteReader returns a reader that repeats the given bytes.
type infiniteReaderType struct{ data []byte }

func infiniteReader(data []byte) io.Reader { return &infiniteReaderType{data: data} }

func (self *infiniteReaderType) Read(p []byte) (int, error) {
	n := copy(p, self.data)
	return n, nil
}
