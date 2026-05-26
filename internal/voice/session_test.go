package voice

import (
	"sync"
	"testing"
	"time"
)

func newTestSession() *Session {
	return NewSession("sess", "conv", "agent", AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1}, AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1}, Features{BargeIn: true}, nil, nil, nil, nil)
}

func TestCloseIdempotent(t *testing.T) {
	s := newTestSession()
	var waitGroup sync.WaitGroup
	for i := 0; i < 10; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			s.Close()
		}()
	}
	waitGroup.Wait()
}

func TestConcurrentStateAccess(t *testing.T) {
	s := newTestSession()
	var waitGroup sync.WaitGroup
	for g := 0; g < 50; g++ {
		waitGroup.Add(1)
		go func(i int) {
			defer waitGroup.Done()
			for j := 0; j < 100; j++ {
				s.SetCurrentRunID("run")
				_ = s.GetCurrentRunID()
				s.SetCurrentResponseID("response")
				_ = s.GetCurrentResponseID()
				s.SetCurrentTurnID("turn")
				_ = s.GetCurrentTurnID()
				s.ClearCurrentRun()
				s.ClearCurrentResponse()
			}
		}(g)
	}
	waitGroup.Wait()
}

func TestNonBlockingEnqueue(t *testing.T) {
	s := newTestSession()
	for i := 0; i < cap(s.audioOutChannel); i++ {
		s.audioOutChannel <- []byte{1}
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
	for i := 0; i < cap(s.audioOutChannel); i++ {
		s.audioOutChannel <- []byte{1}
	}
	start := time.Now()
	s.triggerBargeIn()
	if time.Since(start) > 5*time.Millisecond {
		t.Fatal("triggerBargeIn blocked")
	}
}

func TestTriggerBargeInClearsQueuedSpeechAndQueuesFlush(t *testing.T) {
	s := newTestSession()
	s.SetCurrentRunID("run-1")
	s.SetCurrentResponseID("response-1")

	s.ttsInChannel <- "old sentence"
	s.ttsInChannel <- ""
	s.audioOutChannel <- []byte{1}
	s.audioOutChannel <- []byte{2}

	s.triggerBargeIn()

	if got := len(s.ttsInChannel); got != 0 {
		t.Fatalf("expected empty tts queue after barge-in, got %d", got)
	}
	if got := len(s.audioOutChannel); got != 1 {
		t.Fatalf("expected only flush frame queued after barge-in, got %d items", got)
	}

	frame := <-s.audioOutChannel
	parsed, err := ParseBinaryAudioFrame(frame)
	if err != nil {
		t.Fatalf("expected valid binary frame, got error: %v", err)
	}
	if parsed.FrameType != FrameTypeFlush {
		t.Fatalf("expected flush frame type, got %d", parsed.FrameType)
	}
	if s.GetCurrentRunID() != "" {
		t.Fatal("expected run id cleared after barge-in")
	}
	if s.GetCurrentResponseID() != "" {
		t.Fatal("expected response id cleared after barge-in")
	}
	if !s.IsRunCanceled("run-1") {
		t.Fatal("expected interrupted run tracked as canceled")
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

	var waitGroup sync.WaitGroup
	for i := 0; i < 20; i++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			s.EnqueuePendingTurn("turn", "text")
			if index%2 == 0 {
				s.DequeuePendingTurn()
			}
			s.HasPendingTurns()
		}(i)
	}
	waitGroup.Wait()
}
