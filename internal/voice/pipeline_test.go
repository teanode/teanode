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
	for i := 0; i < 25; i++ {
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

func TestSpeculative_Promoted(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:             true,
		ServerTurn:            true,
		BargeIn:               true,
		SpeculativeLLMEnabled: true,
	})
	registry := deps.registry.(*pipelineMockProviderRegistry)
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
	if !s.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber")
	}
	s.startNewTurn("turn-spec")

	done := make(chan struct{})
	go func() {
		s.streamingTranscribeLoop()
		close(done)
	}()

	interim := "hello world this is a speculative interim transcript"
	stream.events <- VoiceTranscribeEvent{Type: "interim", Text: interim, Confidence: 0.9}
	waitFor(t, time.Second, func() bool { return deps.sendCount() == 1 })
	if !deps.lastSend().IsSpeculative {
		t.Fatal("expected first send to be speculative")
	}

	stream.events <- VoiceTranscribeEvent{Type: "final", Text: interim}
	waitFor(t, time.Second, func() bool {
		return s.GetCurrentRunId() == "run-1" && s.IsTurnCommitted("turn-spec")
	})
	if deps.sendCount() != 1 {
		t.Fatalf("expected exactly one send on speculative promotion, got %d", deps.sendCount())
	}

	close(s.doneCh)
	_ = stream.Close()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("streamingTranscribeLoop did not stop")
	}
}

func TestSpeculative_Cancelled_Diverged(t *testing.T) {
	stream := newPipelineMockTranscribeStream()
	s, deps, _ := newPipelineSessionWithEventsAndFeatures("unused", Features{
		ServerVAD:             true,
		ServerTurn:            true,
		BargeIn:               true,
		SpeculativeLLMEnabled: true,
	})
	registry := deps.registry.(*pipelineMockProviderRegistry)
	registry.streamingTranscriber = &pipelineMockStreamingTranscriber{stream: stream}
	if !s.startStreamingTranscriber() {
		t.Fatal("expected streaming transcriber")
	}
	s.startNewTurn("turn-spec")

	done := make(chan struct{})
	go func() {
		s.streamingTranscribeLoop()
		close(done)
	}()

	stream.events <- VoiceTranscribeEvent{
		Type:       "interim",
		Text:       "hello friend this is a speculative transcript",
		Confidence: 0.95,
	}
	waitFor(t, time.Second, func() bool { return deps.sendCount() == 1 })

	stream.events <- VoiceTranscribeEvent{
		Type: "final",
		Text: "goodbye world this final transcript diverges strongly",
	}
	waitFor(t, time.Second, func() bool { return deps.sendCount() == 2 })
	if deps.cancelCount() == 0 {
		t.Fatal("expected speculative run cancellation on divergence")
	}
	if deps.lastSend().IsSpeculative {
		t.Fatal("expected final committed send to be non-speculative")
	}

	close(s.doneCh)
	_ = stream.Close()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("streamingTranscribeLoop did not stop")
	}
}

func TestSpeculative_CancelledOnBargeIn(t *testing.T) {
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:             true,
		ServerTurn:            true,
		BargeIn:               true,
		SpeculativeLLMEnabled: true,
	})
	s.startSpeculativeRun("hello this interim transcript is long enough")
	waitFor(t, time.Second, func() bool { return deps.sendCount() == 1 })

	s.triggerBargeIn()
	if deps.cancelCount() != 1 {
		t.Fatalf("expected speculative cancel on barge-in, got %d", deps.cancelCount())
	}
}

func TestSpeculative_GuardRail_RecentBargeIn(t *testing.T) {
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:             true,
		ServerTurn:            true,
		BargeIn:               true,
		SpeculativeLLMEnabled: true,
	})
	s.setLastBargeInAt(time.Now())
	s.maybeStartOrRefreshSpeculativeRun(VoiceTranscribeEvent{
		Type:       "interim",
		Text:       "hello this interim transcript is long enough",
		Confidence: 0.95,
	})
	if deps.sendCount() != 0 {
		t.Fatalf("expected no speculative run after recent barge-in, got %d sends", deps.sendCount())
	}
}

func TestSpeculative_GuardRail_NoActiveTurn(t *testing.T) {
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:             true,
		ServerTurn:            true,
		BargeIn:               true,
		SpeculativeLLMEnabled: true,
	})
	s.maybeStartOrRefreshSpeculativeRun(VoiceTranscribeEvent{
		Type:       "interim",
		Text:       "hello this interim transcript is long enough",
		Confidence: 0.95,
	})
	if deps.sendCount() != 0 {
		t.Fatalf("expected no speculative run without active turn, got %d sends", deps.sendCount())
	}
}

func TestSpeculative_Race(t *testing.T) {
	s, deps := newPipelineSessionWithFeatures("unused", Features{
		ServerVAD:             true,
		ServerTurn:            true,
		BargeIn:               true,
		SpeculativeLLMEnabled: true,
	})
	s.startNewTurn("turn-race")
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.maybeStartOrRefreshSpeculativeRun(VoiceTranscribeEvent{
					Type:       "interim",
					Text:       fmt.Sprintf("hello speculative message from goroutine %d iteration %d", i, j),
					Confidence: 0.95,
				})
			}
		}(i)
	}
	wg.Wait()
	if deps.sendCount() == 0 {
		t.Fatal("expected at least one speculative send")
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
