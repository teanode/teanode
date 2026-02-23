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
	text string
}

func (self *pipelineMockTranscriber) Transcribe(_ context.Context, _ VoiceTranscribeRequest) (*VoiceTranscribeResponse, error) {
	return &VoiceTranscribeResponse{Text: self.text}, nil
}

type pipelineMockProviderRegistry struct {
	transcriber          VoiceTranscriber
	streamingTranscriber VoiceStreamingTranscriber
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
	return nil, "", false
}

type pipelineMockStreamingTranscriber struct {
	stream VoiceTranscribeStream
}

func (self *pipelineMockStreamingTranscriber) OpenTranscribeStream(context.Context, VoiceStreamTranscribeRequest) (VoiceTranscribeStream, error) {
	return self.stream, nil
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
	mu         sync.Mutex
	runCounter int
	sendCalls  []VoiceSendMessageParams
	abortCalls []string
	registry   VoiceProviderRegistry
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

func (self *pipelineMockDeps) Subscribe(_ VoiceSubscriber)             {}
func (self *pipelineMockDeps) Unsubscribe(_ VoiceSubscriber)           {}
func (self *pipelineMockDeps) NewConversation(_, _ string) string      { return "conv" }
func (self *pipelineMockDeps) DefaultAgentID() string                  { return "agent" }
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

	if deps.sendCount() != 0 {
		t.Fatalf("expected no immediate send while run active, got %d", deps.sendCount())
	}
	if !s.HasPendingTurns() {
		t.Fatal("expected pending turn queued")
	}
}

func TestCommitNextPendingTurnAfterTerminal(t *testing.T) {
	s, deps := newPipelineSession("hello from queued turn")
	s.SetCurrentRunId("run-active")
	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
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

	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	s.transcribeAndSend("turn-2", makePCMFrame(12000, 320))

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
	for i := 0; i < 25; i++ {
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

func TestSileroVAD_FallbackOnInitError(t *testing.T) {
	t.Setenv("TEANODE_SILERO_URL", "://invalid-endpoint")
	s, _, rec := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:  true,
		ServerTurn: true,
		SileroVAD:  true,
		BargeIn:    true,
	})

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makeFrame(12000, 320)
	for i := 0; i < 12; i++ {
		s.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return rec.findTurnEvent("speech_started") != nil
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
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
