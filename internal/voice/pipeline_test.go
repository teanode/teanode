package voice

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/runners"
)

type mockDispatcher struct {
	mu         sync.Mutex
	runCounter int
	sendCalls  []coordinators.RunParameters
	abortCalls []string
}

func (self *mockDispatcher) Run(_ context.Context, parameters coordinators.RunParameters, _ *runners.RunCallbacks) (*coordinators.RunHandle, error) {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.runCounter++
	self.sendCalls = append(self.sendCalls, parameters)
	handle := coordinators.NewRunHandle(fmt.Sprintf("run-%d", self.runCounter), parameters.ConversationID)
	handle.Resolve(&runners.RunResult{Response: "ok"}, nil)
	return handle, nil
}

func (self *mockDispatcher) AbortRun(runId string) bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	self.abortCalls = append(self.abortCalls, runId)
	return true
}

func (self *mockDispatcher) ProviderRegistry() *providers.ProviderRegistry {
	// Return nil; tests that need provider access use the registry field directly via transcribeAndSend override.
	return nil
}

type eventRecorder struct {
	mu     sync.Mutex
	events []map[string]interface{}
}

func (self *eventRecorder) append(value any) {
	eventMap, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	self.events = append(self.events, eventMap)
}

func (self *eventRecorder) findTurnEvent(event string) map[string]interface{} {
	self.mu.Lock()
	defer self.mu.Unlock()
	for index := len(self.events) - 1; index >= 0; index-- {
		entry := self.events[index]
		if entry["type"] != "turn.event" {
			continue
		}
		payload, _ := entry["payload"].(turnEventPayload)
		if payload.Event == event {
			return entry
		}
	}
	return nil
}

// mockVoiceDispatcher wraps mockDispatcher and provides a real provider registry
// so that transcribeAndSend can find the mock transcriber.
type mockVoiceDispatcher struct {
	*mockDispatcher
	providerRegistry *providers.ProviderRegistry
}

func (self *mockVoiceDispatcher) ProviderRegistry() *providers.ProviderRegistry {
	return self.providerRegistry
}

func newMockProviderRegistryWithTranscriber(text string) *providers.ProviderRegistry {
	providerRegistry := providers.NewProviderRegistry(nil)
	// Register under "openai" to override the default provider so FindTranscriber
	// returns the mock instead of the real OpenAI client.
	providerRegistry.Register("openai", &mockTranscriberProvider{text: text})
	return providerRegistry
}

// mockTranscriberProvider satisfies both providers.Provider and providers.AudioTranscriber.
type mockTranscriberProvider struct {
	text string
}

func (self *mockTranscriberProvider) ChatCompletion(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (self *mockTranscriberProvider) ChatCompletionStream(_ context.Context, _ providers.ChatRequest) (<-chan providers.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func (self *mockTranscriberProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

func (self *mockTranscriberProvider) Transcribe(_ context.Context, _ providers.TranscribeRequest) (*providers.TranscribeResponse, error) {
	return &providers.TranscribeResponse{Text: self.text}, nil
}

func (self *mockDispatcher) sendCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.sendCalls)
}

func (self *mockDispatcher) lastSend() coordinators.RunParameters {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.sendCalls[len(self.sendCalls)-1]
}

func (self *mockDispatcher) abortCount() int {
	self.mu.Lock()
	defer self.mu.Unlock()
	return len(self.abortCalls)
}

func newPipelineSession(text string) (*Session, *mockDispatcher) {
	dispatcher := &mockDispatcher{}
	session := NewSession(
		"sess",
		"conv",
		"agent",
		"",
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1},
		Features{BargeIn: true},
		&mockVoiceDispatcher{mockDispatcher: dispatcher, providerRegistry: newMockProviderRegistryWithTranscriber(text)},
		nil,
		nil,
		nil,
	)
	return session, dispatcher
}

func newPipelineSessionWithEvents(text string) (*Session, *mockDispatcher, *eventRecorder) {
	recorder := &eventRecorder{}
	dispatcher := &mockDispatcher{}
	session := NewSession(
		"sess",
		"conv",
		"agent",
		"",
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1},
		AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1},
		Features{BargeIn: true},
		&mockVoiceDispatcher{mockDispatcher: dispatcher, providerRegistry: newMockProviderRegistryWithTranscriber(text)},
		nil,
		recorder.append,
		nil,
	)
	return session, dispatcher, recorder
}

func makePCMFrame(sample int16, samples int) []byte {
	buf := make([]byte, samples*2)
	for index := 0; index < samples; index++ {
		binary.LittleEndian.PutUint16(buf[index*2:index*2+2], uint16(sample))
	}
	return buf
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestTranscribeQueuesWhenRunActive(t *testing.T) {
	session, dispatcher := newPipelineSession("hello from queued turn")
	session.SetCurrentRunID("run-active")

	session.transcribeAndSend("turn-1", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no immediate send while run active, got %d", dispatcher.sendCount())
	}
	if !session.HasPendingTurns() {
		t.Fatal("expected pending turn queued")
	}
}

func TestCommitNextPendingTurnAfterTerminal(t *testing.T) {
	session, dispatcher := newPipelineSession("hello from queued turn")
	session.SetCurrentRunID("run-active")
	session.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	if !session.HasPendingTurns() {
		t.Fatal("expected queued turn before drain")
	}

	session.ClearCurrentRun()
	session.commitNextPendingTurn()

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
	if session.GetCurrentRunID() == "" {
		t.Fatal("expected run id set after committing drained turn")
	}
}

func TestCommitVoiceTurnIncludesPromptSuffix(t *testing.T) {
	session, dispatcher := newPipelineSession("this should commit now")

	session.transcribeAndSend("turn-commit", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 1 {
		t.Fatalf("expected one send call, got %d", dispatcher.sendCount())
	}
	parameters := dispatcher.lastSend()
	if parameters.SystemPromptSuffix == "" {
		t.Fatal("expected non-empty voice prompt suffix")
	}
}

func TestAudioInputLoopTriggersBargeInWhenRunActive(t *testing.T) {
	session, dispatcher := newPipelineSession("unused")
	session.SetCurrentRunID("run-active")

	finished := make(chan struct{})
	go func() {
		session.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for index := 0; index < 10; index++ {
		session.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return dispatcher.abortCount() > 0 && session.GetCurrentRunID() == ""
	})

	close(session.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}

func TestTranscribeEmptyTextEmitsDroppedReason(t *testing.T) {
	session, dispatcher, recorder := newPipelineSessionWithEvents("   ")
	session.transcribeAndSend("turn-empty", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no send for empty transcript, got %d", dispatcher.sendCount())
	}
	event := recorder.findTurnEvent("turn_dropped")
	if event == nil {
		t.Fatal("expected turn_dropped event")
	}
	payload := event["payload"].(turnEventPayload)
	if payload.Reason != "dropped_empty_transcript" {
		t.Fatalf("expected dropped_empty_transcript reason, got %q", payload.Reason)
	}
}

func TestQueueOverflowDropsOldestWithReason(t *testing.T) {
	session, dispatcher, recorder := newPipelineSessionWithEvents("queued transcript text")
	session.maxPendingTurns = 1
	session.SetCurrentRunID("run-active")

	session.transcribeAndSend("turn-1", makePCMFrame(12000, 320))
	session.transcribeAndSend("turn-2", makePCMFrame(12000, 320))

	if dispatcher.sendCount() != 0 {
		t.Fatalf("expected no sends while run active, got %d", dispatcher.sendCount())
	}
	event := recorder.findTurnEvent("turn_dropped")
	if event == nil {
		t.Fatal("expected overflow turn_dropped event")
	}
	payload := event["payload"].(turnEventPayload)
	if payload.Reason != "dropped_queue_overflow" {
		t.Fatalf("expected dropped_queue_overflow reason, got %q", payload.Reason)
	}
}

func TestAudioInputLoopTriggersBargeInWhenResponseActive(t *testing.T) {
	session, dispatcher := newPipelineSession("unused")
	session.SetCurrentResponseID("response-active")

	finished := make(chan struct{})
	go func() {
		session.audioInputLoop()
		close(finished)
	}()

	loud := makePCMFrame(12000, 320)
	for index := 0; index < 10; index++ {
		session.audioInCh <- loud
	}

	waitFor(t, 500*time.Millisecond, func() bool {
		return dispatcher.abortCount() == 0 && session.GetCurrentResponseID() == ""
	})

	close(session.doneCh)
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("audioInputLoop did not stop after done")
	}
}
