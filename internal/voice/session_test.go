package voice

import (
	"sync"
	"testing"
	"time"
)

type stubDeps struct{}

func (s *stubDeps) SendMessage(_ interface{}, _ VoiceSendMessageParams) VoiceRunHandle {
	return VoiceRunHandle{}
}
func (s *stubDeps) AbortRun(_ string) bool                  { return true }
func (s *stubDeps) Subscribe(_ VoiceSubscriber)             {}
func (s *stubDeps) Unsubscribe(_ VoiceSubscriber)           {}
func (s *stubDeps) NewConversation(_, _ string) string      { return "conv" }
func (s *stubDeps) DefaultAgentID() string                  { return "agent" }
func (s *stubDeps) ProviderRegistry() VoiceProviderRegistry { return nil }

func newTestSession() *Session {
	return NewSession("sess", "conv", "agent", "", AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1}, AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1}, Features{BargeIn: true}, nil, nil, nil)
}

func TestCloseIdempotent(t *testing.T) {
	s := newTestSession()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Close()
		}()
	}
	wg.Wait()
}

func TestConcurrentStateAccess(t *testing.T) {
	s := newTestSession()
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.SetCurrentRunID("run")
				_ = s.GetCurrentRunID()
				s.SetCurrentResponseID("resp")
				_ = s.GetCurrentResponseID()
				s.SetCurrentTurnID("turn")
				_ = s.GetCurrentTurnID()
				s.ClearCurrentRun()
				s.ClearCurrentResponse()
			}
		}(g)
	}
	wg.Wait()
}

func TestNonBlockingEnqueue(t *testing.T) {
	s := newTestSession()
	for i := 0; i < cap(s.audioOutCh); i++ {
		s.audioOutCh <- []byte{1}
	}
	start := time.Now()
	ok := s.enqueueAudioOut([]byte{2})
	if ok {
		t.Fatal("expected enqueue to fail when full")
	}
	if time.Since(start) > 5*time.Millisecond {
		t.Fatal("enqueue blocked unexpectedly")
	}
}

func TestTriggerBargeInNonBlocking(t *testing.T) {
	s := newTestSession()
	s.SetCurrentRunID("run-1")
	for i := 0; i < cap(s.audioOutCh); i++ {
		s.audioOutCh <- []byte{1}
	}
	start := time.Now()
	s.triggerBargeIn()
	if time.Since(start) > 5*time.Millisecond {
		t.Fatal("triggerBargeIn blocked")
	}
}

func TestPendingTurnQueueFIFOAndOverflow(t *testing.T) {
	s := newTestSession()
	s.maxPendingTurns = 2

	dropped, depth := s.EnqueuePendingTurn("t1", "first")
	if dropped != nil || depth != 1 {
		t.Fatalf("unexpected first enqueue result: dropped=%v depth=%d", dropped, depth)
	}
	dropped, depth = s.EnqueuePendingTurn("t2", "second")
	if dropped != nil || depth != 2 {
		t.Fatalf("unexpected second enqueue result: dropped=%v depth=%d", dropped, depth)
	}
	dropped, depth = s.EnqueuePendingTurn("t3", "third")
	if dropped == nil || dropped.TurnID != "t1" {
		t.Fatalf("expected oldest turn t1 dropped, got %+v", dropped)
	}
	if depth != 2 {
		t.Fatalf("expected queue depth 2 after overflow, got %d", depth)
	}

	first, ok := s.DequeuePendingTurn()
	if !ok || first.TurnID != "t2" {
		t.Fatalf("expected first dequeue t2, got %+v", first)
	}
	second, ok := s.DequeuePendingTurn()
	if !ok || second.TurnID != "t3" {
		t.Fatalf("expected second dequeue t3, got %+v", second)
	}
	if s.HasPendingTurns() {
		t.Fatal("expected pending queue empty")
	}
}

func TestPendingTurnQueueConcurrentAccess(t *testing.T) {
	s := newTestSession()
	s.maxPendingTurns = 8

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.EnqueuePendingTurn("turn", "text")
			if idx%2 == 0 {
				s.DequeuePendingTurn()
			}
			s.HasPendingTurns()
		}(i)
	}
	wg.Wait()
}
