package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"
)

type pipelineMockTranscriber struct {
	mu    sync.Mutex
	text  string
	delay time.Duration
	calls int
}

func (self *pipelineMockTranscriber) Transcribe(_ context.Context, _ VoiceTranscribeRequest) (*VoiceTranscribeResponse, error) {
	self.mu.Lock()
	self.calls++
	delay := self.delay
	text := self.text
	self.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return &VoiceTranscribeResponse{Text: text}, nil
}

func (self *pipelineMockTranscriber) callCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.calls
}

type pipelineMockProviderRegistry struct {
	transcriber          VoiceTranscriber
	streamingTranscriber VoiceStreamingTranscriber
	synthesizer          VoiceSynthesizer
}

func (self *pipelineMockProviderRegistry) FindTranscriber() (VoiceTranscriber, string, bool) {
	if self.transcriber == nil {
		return nil, "", false
	}
	return self.transcriber, "mock", true
}

func (self *pipelineMockProviderRegistry) FindStreamingTranscriber() (VoiceStreamingTranscriber, string, bool) {
	if self.streamingTranscriber == nil {
		return nil, "", false
	}
	return self.streamingTranscriber, "mock-stream", true
}

func (self *pipelineMockProviderRegistry) FindSynthesizer() (VoiceSynthesizer, string, bool) {
	if self.synthesizer == nil {
		return nil, "", false
	}
	return self.synthesizer, "mock-tts", true
}

type pipelineMockStreamingTranscriber struct {
	stream VoiceTranscribeStream
}

func (self *pipelineMockStreamingTranscriber) OpenTranscribeStream(context.Context, VoiceStreamTranscribeRequest) (VoiceTranscribeStream, error) {
	return self.stream, nil
}

type pipelineMockSynthesizer struct {
	synthesizeFn       func(context.Context, string, string, int) ([]byte, error)
	synthesizeStreamFn func(context.Context, string, string, int) (<-chan []byte, error)
}

func (self *pipelineMockSynthesizer) SynthesizePCM(ctx context.Context, text, voice string, sampleRateHz int) ([]byte, error) {
	if self.synthesizeFn != nil {
		return self.synthesizeFn(ctx, text, voice, sampleRateHz)
	}
	return nil, nil
}

func (self *pipelineMockSynthesizer) SynthesizePCMStream(ctx context.Context, text, voice string, sampleRateHz int) (<-chan []byte, error) {
	if self.synthesizeStreamFn != nil {
		return self.synthesizeStreamFn(ctx, text, voice, sampleRateHz)
	}
	out := make(chan []byte, 1)
	if audio, err := self.SynthesizePCM(ctx, text, voice, sampleRateHz); err == nil && len(audio) > 0 {
		out <- audio
	}
	close(out)
	return out, nil
}

type pipelineMockTranscribeStream struct {
	events chan VoiceTranscribeEvent
}

func newPipelineMockTranscribeStream() *pipelineMockTranscribeStream {
	return &pipelineMockTranscribeStream{events: make(chan VoiceTranscribeEvent, 8)}
}

func (self *pipelineMockTranscribeStream) SendAudio([]byte) error { return nil }
func (self *pipelineMockTranscribeStream) Events() <-chan VoiceTranscribeEvent {
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

type pipelineMockDeps struct {
	mu          sync.Mutex
	runCounter  int
	sendCalls   []VoiceSendMessageParams
	abortCalls  []string
	cancelCalls []string
	registry    VoiceProviderRegistry
}

type eventRecorder struct {
	mu     sync.Mutex
	events []map[string]interface{}
}

func (self *eventRecorder) append(v any) {
	m, ok := v.(map[string]interface{})
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
		e := self.events[i]
		if e["type"] != "turn.event" {
			continue
		}
		if payload, ok := e["payload"].(turnEventPayload); ok {
			if payload.Event == event {
				return e
			}
			continue
		}
		if payload, ok := e["payload"].(map[string]interface{}); ok {
			name, _ := payload["event"].(string)
			if name == event {
				return e
			}
		}
	}
	return nil
}

func (self *pipelineMockDeps) SendMessage(_ context.Context, parameters VoiceSendMessageParams) VoiceRunHandle {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.runCounter++
	self.sendCalls = append(self.sendCalls, parameters)
	done := make(chan struct{})
	close(done)
	return VoiceRunHandle{RunID: fmt.Sprintf("run-%d", self.runCounter), Done: done}
}

func (self *pipelineMockDeps) AbortRun(runId string) bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.abortCalls = append(self.abortCalls, runId)
	return true
}

func (self *pipelineMockDeps) CancelRun(runId string) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.cancelCalls = append(self.cancelCalls, runId)
}

func (self *pipelineMockDeps) Subscribe(_ VoiceSubscriber)             {}
func (self *pipelineMockDeps) Unsubscribe(_ VoiceSubscriber)           {}
func (self *pipelineMockDeps) ProviderRegistry() VoiceProviderRegistry { return self.registry }

func (self *pipelineMockDeps) sendCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.sendCalls)
}

func (self *pipelineMockDeps) lastSend() VoiceSendMessageParams {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.sendCalls[len(self.sendCalls)-1]
}

func (self *pipelineMockDeps) abortCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.abortCalls)
}

func (self *pipelineMockDeps) cancelCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.cancelCalls)
}

func newPipelineSessionWithFeatures(text string, features Features) (*Session, *pipelineMockDeps) {
	deps := &pipelineMockDeps{
		registry: &pipelineMockProviderRegistry{
			transcriber: &pipelineMockTranscriber{text: text},
		},
	}
	s := NewSession(
		"sess",
		"conv",
		"agent",
		"",
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1},
		features,
		deps,
		nil,
		nil,
	)
	return s, deps
}

func newPipelineSession(text string) (*Session, *pipelineMockDeps) {
	return newPipelineSessionWithFeatures(text, Features{BargeIn: true, ServerVAD: true, ServerTurn: true})
}

func newPipelineSessionWithEventsAndFeatures(text string, features Features) (*Session, *pipelineMockDeps, *eventRecorder) {
	rec := &eventRecorder{}
	deps := &pipelineMockDeps{
		registry: &pipelineMockProviderRegistry{
			transcriber: &pipelineMockTranscriber{text: text},
		},
	}
	s := NewSession(
		"sess",
		"conv",
		"agent",
		"",
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1},
		features,
		deps,
		rec.append,
		nil,
	)
	return s, deps, rec
}

func newPipelineSessionWithEvents(text string) (*Session, *pipelineMockDeps, *eventRecorder) {
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
	s, deps := newPipelineSession("hello from queued turn")
	s.SetCurrentRunId("run-active")

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	waitFor(t, 500*time.Millisecond, func() bool { return deps.sendCount() == 1 })

	if deps.cancelCount() != 1 {
		t.Fatalf("expected one cancel call for preempted run, got %d", deps.cancelCount())
	}
	if s.HasPendingTurns() {
		t.Fatal("expected queued turn to be committed immediately after preemption")
	}
}

func TestTranscribePreemptsQueuedTurnBeforeResponseStarts(t *testing.T) {
	s, deps := newPipelineSession("hello from queued turn")
	s.SetCurrentRunId("run-active")

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	waitFor(t, 500*time.Millisecond, func() bool { return deps.sendCount() == 1 })

	if deps.cancelCount() != 1 {
		t.Fatalf("expected one cancel call for preempted run, got %d", deps.cancelCount())
	}
	if s.HasPendingTurns() {
		t.Fatal("expected queued turn to be committed immediately after preemption")
	}
	parameters := deps.lastSend()
	if parameters.Message != "hello from queued turn" {
		t.Fatalf("unexpected committed message after preemption: %q", parameters.Message)
	}
}

func TestTranscribeDoesNotPreemptWhenResponseAlreadyStarted(t *testing.T) {
	s, deps := newPipelineSession("hello from queued turn")
	s.SetCurrentRunId("run-active")
	s.SetCurrentResponseId("response-active")

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))

	if deps.sendCount() != 1 {
		t.Fatalf("expected immediate send after barge-in when response active, got %d", deps.sendCount())
	}
	if deps.cancelCount() != 0 {
		t.Fatalf("expected no run cancel while response active, got %d", deps.cancelCount())
	}
	if deps.abortCount() != 1 {
		t.Fatalf("expected one abort call while response active, got %d", deps.abortCount())
	}
	if s.HasPendingTurns() {
		t.Fatal("expected no pending turn after response-active barge-in path")
	}
}

func TestCommitNextPendingTurnAfterTerminal(t *testing.T) {
	s, deps := newPipelineSession("hello from queued turn")
	s.SetCurrentRunId("run-active")
	s.EnqueuePendingTurn("turn-1", "hello from queued turn")
	if !s.HasPendingTurns() {
		t.Fatal("expected queued turn before drain")
	}

	s.ClearCurrentRun()
	s.commitNextPendingTurn()

	if deps.sendCount() != 1 {
		t.Fatalf("expected exactly one send after drain, got %d", deps.sendCount())
	}
	parameters := deps.lastSend()
	if parameters.Message != "hello from queued turn" {
		t.Fatalf("unexpected drained message %q", parameters.Message)
	}
	if parameters.SystemPromptSuffix == "" {
		t.Fatal("expected voice system prompt suffix on committed turn")
	}
	if s.GetCurrentRunId() == "" {
		t.Fatal("expected run id set after committing drained turn")
	}
}

func TestCommitVoiceTurnIncludesPromptSuffix(t *testing.T) {
	s, deps := newPipelineSession("this should commit now")

	s.transcribeAndSend("turn-commit", makePCMFrame(12000, 320))

	if deps.sendCount() != 1 {
		t.Fatalf("expected one send call, got %d", deps.sendCount())
	}
	parameters := deps.lastSend()
	if parameters.SystemPromptSuffix == "" {
		t.Fatal("expected non-empty voice prompt suffix")
	}
}

func TestAudioInputLoopTriggersBargeInWhenRunActive(t *testing.T) {
	s, deps := newPipelineSession("unused")
	s.SetCurrentRunId("run-active")

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
		return deps.abortCount() > 0 && s.GetCurrentRunId() == ""
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestTranscribeEmptyTextEmitsDroppedReason(t *testing.T) {
	s, deps, rec := newPipelineSessionWithEvents("   ")
	s.transcribeAndSend("turn-empty", makePCMFrame(12000, 320))

	if deps.sendCount() != 0 {
		t.Fatalf("expected no send for empty transcript, got %d", deps.sendCount())
	}
	ev := rec.findTurnEvent("turn_dropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "dropped_empty_transcript" {
		t.Fatalf("expected dropped_empty_transcript reason, got %q", payload.Reason)
	}
}

func TestQueueOverflowDropsOldestWithReason(t *testing.T) {
	s, deps, rec := newPipelineSessionWithEvents("queued transcript text")
	s.maxPendingTurns = 1
	s.SetCurrentRunId("run-active")
	s.SetCurrentResponseId("response-active")

	s.enqueueTranscriptTurn("turn-1", "queued transcript text")
	s.enqueueTranscriptTurn("turn-2", "queued transcript text")

	if deps.sendCount() != 0 {
		t.Fatalf("expected no sends while run active, got %d", deps.sendCount())
	}
	ev := rec.findTurnEvent("turn_dropped")
	if ev == nil {
		t.Fatal("expected overflow turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "dropped_queue_overflow" {
		t.Fatalf("expected dropped_queue_overflow reason, got %q", payload.Reason)
	}
}

func TestAudioInputLoopTriggersBargeInWhenResponseActive(t *testing.T) {
	s, deps := newPipelineSession("unused")
	s.SetCurrentResponseId("response-active")

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
		return deps.abortCount() == 0 && s.GetCurrentResponseId() == ""
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
	s.SetCurrentRunId("run-active")

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
		if rec.findTurnEvent("barge_in_candidate") != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.findTurnEvent("barge_in_candidate") == nil {
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
	s.SetCurrentRunId("run-active")

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
		if rec.findTurnEvent("barge_in_suppressed") != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.findTurnEvent("barge_in_suppressed") == nil {
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
	if event := rec.findTurnEvent("speech_started"); event != nil {
		t.Fatal("speech_started should not be emitted when ServerVAD=false")
	}
	if event := rec.findTurnEvent("speech_ended"); event != nil {
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
	s, deps, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
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
	if deps.sendCount() != 0 {
		t.Fatalf("expected no automatic SendMessage when ServerTurn=false, got %d", deps.sendCount())
	}
	if event := rec.findTurnEvent("turn_committed"); event != nil {
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
	s, deps, rec := newPipelineSessionWithEventsAndFeatures("hello from commit", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.accumulateExplicitAudio(makePCMFrame(12000, 8000)) // 500 ms

	s.InputCommit("push_to_talk")
	waitFor(t, 500*time.Millisecond, func() bool { return deps.sendCount() == 1 })

	if rec.findTurnEvent("input_committed") == nil {
		t.Fatal("expected input_committed event")
	}
	if deps.sendCount() != 1 {
		t.Fatalf("expected one send after input commit, got %d", deps.sendCount())
	}
}

func TestInputCommit_EmptyBuffer(t *testing.T) {
	s, deps, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})

	s.InputCommit("push_to_talk")
	time.Sleep(50 * time.Millisecond)

	if deps.sendCount() != 0 {
		t.Fatalf("expected no send for empty commit, got %d", deps.sendCount())
	}
	ev := rec.findTurnEvent("turn_dropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "dropped_empty_audio" {
		t.Fatalf("expected dropped_empty_audio reason, got %q", payload.Reason)
	}
}

func TestInputCommit_TooShort(t *testing.T) {
	s, deps, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  false,
		ServerTurn: true,
		BargeIn:    true,
	})
	s.accumulateExplicitAudio(makePCMFrame(12000, 1600)) // 100 ms

	s.InputCommit("push_to_talk")
	time.Sleep(50 * time.Millisecond)

	if deps.sendCount() != 0 {
		t.Fatalf("expected no send for short commit, got %d", deps.sendCount())
	}
	ev := rec.findTurnEvent("turn_dropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "dropped_too_short_audio" {
		t.Fatalf("expected dropped_too_short_audio reason, got %q", payload.Reason)
	}
}

func TestInputCommit_RaceCondition(t *testing.T) {
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("race commit", Features{
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

	waitFor(t, 500*time.Millisecond, func() bool { return deps.sendCount() >= 1 || s.HasPendingTurns() })
	if deps.sendCount() > 1 {
		t.Fatalf("expected at most one send after concurrent commits, got %d", deps.sendCount())
	}
}

func TestCommitCapturedTurn_PrefersStreamingFinalText(t *testing.T) {
	s, deps := newPipelineSession("batch transcript should not be used")
	mockRegistry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	mockTranscriber, ok := mockRegistry.transcriber.(*pipelineMockTranscriber)
	if !ok {
		t.Fatal("expected pipeline mock transcriber")
	}
	s.setStreamingTranscribeStream(newPipelineMockTranscribeStream())
	s.setStreamingFinalText("turn-stream", "I want to learn about GitHub specifically worktree.")

	s.commitCapturedTurn("turn-stream", makePCMFrame(12000, 8000))
	waitFor(t, 800*time.Millisecond, func() bool { return deps.sendCount() == 1 })

	if deps.lastSend().Message != "I want to learn about GitHub specifically worktree." {
		t.Fatalf("unexpected committed transcript: %q", deps.lastSend().Message)
	}
	if mockTranscriber.callCount() != 0 {
		t.Fatalf("expected no batch transcription call when streaming final is strong, got %d", mockTranscriber.callCount())
	}
}

func TestCommitCapturedTurn_FallsBackToBatchWhenStreamingFinalTooShort(t *testing.T) {
	s, deps := newPipelineSession("I am planning a trip to Cheesecake Factory tell me the menu")
	mockRegistry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	mockTranscriber, ok := mockRegistry.transcriber.(*pipelineMockTranscriber)
	if !ok {
		t.Fatal("expected pipeline mock transcriber")
	}
	s.setStreamingTranscribeStream(newPipelineMockTranscribeStream())
	s.setStreamingFinalText("turn-short", "trip")

	s.commitCapturedTurn("turn-short", makePCMFrame(12000, 8000))
	waitFor(t, 1200*time.Millisecond, func() bool { return deps.sendCount() == 1 })

	if deps.lastSend().Message != "I am planning a trip to Cheesecake Factory tell me the menu" {
		t.Fatalf("unexpected committed transcript: %q", deps.lastSend().Message)
	}
	if mockTranscriber.callCount() == 0 {
		t.Fatal("expected batch fallback transcription for short streaming final")
	}
}

func TestStreamingTranscribeLoop_FallbackOnError(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
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
	stream.events <- VoiceTranscribeEvent{Err: context.DeadlineExceeded}
	for i := 0; i < 40; i++ {
		s.audioInCh <- silence
	}

	waitFor(t, 2*time.Second, func() bool { return deps.sendCount() == 1 })
	if deps.lastSend().Message != "fallback transcript" {
		t.Fatalf("unexpected fallback transcript: %q", deps.lastSend().Message)
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
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.synthesizer = &pipelineMockSynthesizer{
		synthesizeStreamFn: func(context.Context, string, string, int) (<-chan []byte, error) {
			out := make(chan []byte, 2)
			out <- []byte{1, 2}
			out <- []byte{3}
			close(out)
			return out, nil
		},
	}

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
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.synthesizer = &pipelineMockSynthesizer{
		synthesizeFn: func(context.Context, string, string, int) ([]byte, error) {
			return []byte{9, 8, 7}, nil
		},
	}

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
	if len(parsed.Data) != 3 {
		t.Fatalf("expected one batch fallback chunk, got %d bytes", len(parsed.Data))
	}

	close(s.doneCh)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ttsSynthLoop did not stop after done")
	}
}

func TestTTSSynthLoop_WaitsWhileUserSpeaking(t *testing.T) {
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.synthesizer = &pipelineMockSynthesizer{
		synthesizeStreamFn: func(context.Context, string, string, int) (<-chan []byte, error) {
			out := make(chan []byte, 1)
			out <- []byte{1, 2, 3}
			close(out)
			return out, nil
		},
	}

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
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}

	started := make(chan struct{})
	streamDone := make(chan struct{})
	registry.synthesizer = &pipelineMockSynthesizer{
		synthesizeStreamFn: func(ctx context.Context, _ string, _ string, _ int) (<-chan []byte, error) {
			out := make(chan []byte)
			go func() {
				close(started)
				defer close(streamDone)
				<-ctx.Done()
				close(out)
			}()
			return out, nil
		},
	}

	loopDone := make(chan struct{})
	go func() {
		s.ttsSynthLoop()
		close(loopDone)
	}()

	s.SetCurrentResponseId("resp-active")
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
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
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

	waitFor(t, 2*time.Second, func() bool { return deps.sendCount() == 1 })
	if deps.lastSend().Message != "fallback transcript" {
		t.Fatalf("unexpected fallback transcript: %q", deps.lastSend().Message)
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
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	mockTranscriber, ok := registry.transcriber.(*pipelineMockTranscriber)
	if !ok {
		t.Fatal("expected pipeline mock transcriber")
	}
	mockTranscriber.delay = 250 * time.Millisecond
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
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

	waitFor(t, 2*time.Second, func() bool { return deps.sendCount() == 1 })

	close(s.doneCh)
	select {
	case <-audioDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestAudioInputLoop_StreamingInterimFallsBackToBatch(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("fallback transcript", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := deps.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
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
	stream.events <- VoiceTranscribeEvent{
		Type:       "interim",
		Text:       "interim transcript should not bypass batch",
		Confidence: 0.95,
	}
	for i := 0; i < vadRedemptionFrames+5; i++ {
		s.audioInCh <- silence
	}

	waitFor(t, 2*time.Second, func() bool { return deps.sendCount() == 1 })
	if deps.lastSend().Message != "fallback transcript" {
		t.Fatalf("unexpected transcript source: %q", deps.lastSend().Message)
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
	session, dependencies, _ := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		BargeIn:    true,
	})
	registry, ok := dependencies.registry.(*pipelineMockProviderRegistry)
	if !ok {
		t.Fatal("expected pipeline mock provider registry")
	}
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
	if !session.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber to start")
	}
	turnId := "turn-boundary"
	session.SetCurrentTurnId(turnId)

	done := make(chan struct{})
	go func() {
		session.streamingTranscribeLoop()
		close(done)
	}()

	stream.events <- VoiceTranscribeEvent{Type: "speech_final", Text: "this is complete"}
	stream.events <- VoiceTranscribeEvent{Type: "utterance_end"}
	waitFor(t, 500*time.Millisecond, func() bool {
		return session.hasStreamingSpeechFinal(turnId) && session.hasStreamingUtteranceEnd(turnId)
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
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:    true,
		ServerTurn:   true,
		BargeIn:      true,
		TurnStrategy: "balanced",
	})
	s.SetCurrentRunId("run-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	for i := 0; i < 12; i++ {
		s.audioInCh <- makePCMFrame(12000, 320)
	}
	waitFor(t, 500*time.Millisecond, func() bool {
		return deps.abortCount() > 0 && s.GetCurrentRunId() == ""
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
