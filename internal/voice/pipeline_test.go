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
	transcriber VoiceTranscriber
}

func (self *pipelineMockProviderRegistry) FindTranscriber() (VoiceTranscriber, string, bool) {
	if self.transcriber == nil {
		return nil, "", false
	}
	return self.transcriber, "mock", true
}

func (self *pipelineMockProviderRegistry) FindSynthesizer() (VoiceSynthesizer, string, bool) {
	return nil, "", false
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
		payload, _ := e["payload"].(turnEventPayload)
		if payload.Event == event {
			return e
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
