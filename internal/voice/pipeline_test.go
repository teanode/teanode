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

func (m *pipelineMockTranscriber) Transcribe(_ context.Context, _ VoiceTranscribeRequest) (*VoiceTranscribeResponse, error) {
	return &VoiceTranscribeResponse{Text: m.text}, nil
}

type pipelineMockProviderRegistry struct {
	transcriber VoiceTranscriber
}

func (m *pipelineMockProviderRegistry) FindTranscriber() (VoiceTranscriber, string, bool) {
	if m.transcriber == nil {
		return nil, "", false
	}
	return m.transcriber, "mock", true
}

func (m *pipelineMockProviderRegistry) FindSynthesizer() (VoiceSynthesizer, string, bool) {
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

func (r *eventRecorder) append(v any) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, m)
}

func (r *eventRecorder) findTurnEvent(event string) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		e := r.events[i]
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

func (m *pipelineMockDeps) SendMessage(_ context.Context, params VoiceSendMessageParams) VoiceRunHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runCounter++
	m.sendCalls = append(m.sendCalls, params)
	done := make(chan struct{})
	close(done)
	return VoiceRunHandle{RunID: fmt.Sprintf("run-%d", m.runCounter), Done: done}
}

func (m *pipelineMockDeps) AbortRun(runID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.abortCalls = append(m.abortCalls, runID)
	return true
}

func (m *pipelineMockDeps) Subscribe(_ VoiceSubscriber)             {}
func (m *pipelineMockDeps) Unsubscribe(_ VoiceSubscriber)           {}
func (m *pipelineMockDeps) NewConversation(_, _ string) string      { return "conv" }
func (m *pipelineMockDeps) DefaultAgentID() string                  { return "agent" }
func (m *pipelineMockDeps) ProviderRegistry() VoiceProviderRegistry { return m.registry }

func (m *pipelineMockDeps) sendCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sendCalls)
}

func (m *pipelineMockDeps) lastSend() VoiceSendMessageParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendCalls[len(m.sendCalls)-1]
}

func (m *pipelineMockDeps) abortCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.abortCalls)
}

func newPipelineSession(text string) (*Session, *pipelineMockDeps) {
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
		Features{BargeIn: true},
		deps,
		nil,
		nil,
	)
	return s, deps
}

func newPipelineSessionWithEvents(text string) (*Session, *pipelineMockDeps, *eventRecorder) {
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
		Features{BargeIn: true},
		deps,
		rec.append,
		nil,
	)
	return s, deps, rec
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
	s.SetCurrentRunID("run-active")

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
	s.SetCurrentRunID("run-active")
	s.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	if !s.HasPendingTurns() {
		t.Fatal("expected queued turn before drain")
	}

	s.ClearCurrentRun()
	s.commitNextPendingTurn()

	if deps.sendCount() != 1 {
		t.Fatalf("expected exactly one send after drain, got %d", deps.sendCount())
	}
	params := deps.lastSend()
	if params.Message != "hello from queued turn" {
		t.Fatalf("unexpected drained message %q", params.Message)
	}
	if params.SystemPromptSuffix == "" {
		t.Fatal("expected voice system prompt suffix on committed turn")
	}
	if s.GetCurrentRunID() == "" {
		t.Fatal("expected run id set after committing drained turn")
	}
}

func TestCommitVoiceTurnIncludesPromptSuffix(t *testing.T) {
	s, deps := newPipelineSession("this should commit now")

	s.transcribeAndSend("turn-commit", makePCMFrame(12000, 320))

	if deps.sendCount() != 1 {
		t.Fatalf("expected one send call, got %d", deps.sendCount())
	}
	params := deps.lastSend()
	if params.SystemPromptSuffix == "" {
		t.Fatal("expected non-empty voice prompt suffix")
	}
}

func TestAudioInputLoopTriggersBargeInWhenRunActive(t *testing.T) {
	s, deps := newPipelineSession("unused")
	s.SetCurrentRunID("run-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for i := 0; i < 7; i++ {
		s.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return deps.abortCount() > 0 && s.GetCurrentRunID() == ""
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestTranscribeTooShortTextEmitsDroppedReason(t *testing.T) {
	s, deps, rec := newPipelineSessionWithEvents("ok")
	s.transcribeAndSend("turn-short", makePCMFrame(12000, 320))

	if deps.sendCount() != 0 {
		t.Fatalf("expected no send for short transcript, got %d", deps.sendCount())
	}
	ev := rec.findTurnEvent("turn_dropped")
	if ev == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := ev["payload"].(turnEventPayload)
	if payload.Reason != "dropped_too_short_text" {
		t.Fatalf("expected dropped_too_short_text reason, got %q", payload.Reason)
	}
}

func TestQueueOverflowDropsOldestWithReason(t *testing.T) {
	s, deps, rec := newPipelineSessionWithEvents("queued transcript text")
	s.maxPendingTurns = 1
	s.SetCurrentRunID("run-active")

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
	s.SetCurrentResponseID("resp-active")

	finished := make(chan struct{})
	go func() {
		s.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for i := 0; i < 7; i++ {
		s.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return deps.abortCount() == 0 && s.GetCurrentResponseID() == ""
	})

	close(s.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}
