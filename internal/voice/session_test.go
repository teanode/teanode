package voice

import (
	"sync"
	"testing"
	"time"
)

func newTestSession() *Session {
	return NewSession("sess", "conv", "agent", "", AudioFormat{Codec: "pcm_s16le", SampleRateHz: 16000, Channels: 1}, AudioFormat{Codec: "pcm_s16le", SampleRateHz: 24000, Channels: 1}, Features{BargeIn: true}, nil, nil, nil, nil)
}

func TestCloseIdempotent(t *testing.T) {
	session := newTestSession()
	var waitGroup sync.WaitGroup
	for index := 0; index < 10; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			session.Close()
		}()
	}
	waitGroup.Wait()
}

func TestConcurrentStateAccess(t *testing.T) {
	session := newTestSession()
	var waitGroup sync.WaitGroup
	for goroutine := 0; goroutine < 50; goroutine++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			for iteration := 0; iteration < 100; iteration++ {
				session.SetCurrentRunID("run")
				_ = session.GetCurrentRunID()
				session.SetCurrentResponseID("response")
				_ = session.GetCurrentResponseID()
				session.SetCurrentTurnID("turn")
				_ = session.GetCurrentTurnID()
				session.ClearCurrentRun()
				session.ClearCurrentResponse()
			}
		}(goroutine)
	}
	waitGroup.Wait()
}

func TestNonBlockingEnqueue(t *testing.T) {
	session := newTestSession()
	for index := 0; index < cap(session.audioOutCh); index++ {
		session.audioOutCh <- []byte{1}
	}
	start := time.Now()
	ok := session.enqueueAudioOut([]byte{2})
	if ok {
		t.Fatal("expected enqueue to fail when full")
	}
	if time.Since(start) > 5*time.Millisecond {
		t.Fatal("enqueue blocked unexpectedly")
	}
}

func TestTriggerBargeInNonBlocking(t *testing.T) {
	session := newTestSession()
	session.SetCurrentRunID("run-1")
	for index := 0; index < cap(session.audioOutCh); index++ {
		session.audioOutCh <- []byte{1}
	}
	start := time.Now()
	session.triggerBargeIn()
	if time.Since(start) > 5*time.Millisecond {
		t.Fatal("triggerBargeIn blocked")
	}
}

func TestTriggerBargeInClearsQueuedSpeechAndQueuesFlush(t *testing.T) {
	session := newTestSession()
	session.SetCurrentRunID("run-1")
	session.SetCurrentResponseID("resp-1")

	session.ttsInCh <- "old sentence"
	session.ttsInCh <- ""
	session.audioOutCh <- []byte{1}
	session.audioOutCh <- []byte{2}

	session.triggerBargeIn()

	if got := len(session.ttsInCh); got != 0 {
		t.Fatalf("expected empty tts queue after barge-in, got %d", got)
	}
	if got := len(session.audioOutCh); got != 1 {
		t.Fatalf("expected only flush frame queued after barge-in, got %d items", got)
	}

	frame := <-session.audioOutCh
	parsed, err := ParseBinaryAudioFrame(frame)
	if err != nil {
		t.Fatalf("expected valid binary frame, got error: %v", err)
	}
	if parsed.FrameType != FrameTypeFlush {
		t.Fatalf("expected flush frame type, got %d", parsed.FrameType)
	}
	if session.GetCurrentRunID() != "" {
		t.Fatal("expected run id cleared after barge-in")
	}
	if session.GetCurrentResponseID() != "" {
		t.Fatal("expected response id cleared after barge-in")
	}
	if !session.IsRunCanceled("run-1") {
		t.Fatal("expected interrupted run tracked as canceled")
	}
}

func TestPendingTurnQueueFIFOAndOverflow(t *testing.T) {
	session := newTestSession()
	session.maxPendingTurns = 2

	dropped, depth := session.EnqueuePendingTurn("t1", "first")
	if dropped != nil || depth != 1 {
		t.Fatalf("unexpected first enqueue result: dropped=%v depth=%d", dropped, depth)
	}
	dropped, depth = session.EnqueuePendingTurn("t2", "second")
	if dropped != nil || depth != 2 {
		t.Fatalf("unexpected second enqueue result: dropped=%v depth=%d", dropped, depth)
	}
	dropped, depth = session.EnqueuePendingTurn("t3", "third")
	if dropped == nil || dropped.TurnID != "t1" {
		t.Fatalf("expected oldest turn t1 dropped, got %+v", dropped)
	}
	if depth != 2 {
		t.Fatalf("expected queue depth 2 after overflow, got %d", depth)
	}

	first, ok := session.DequeuePendingTurn()
	if !ok || first.TurnID != "t2" {
		t.Fatalf("expected first dequeue t2, got %+v", first)
	}
	second, ok := session.DequeuePendingTurn()
	if !ok || second.TurnID != "t3" {
		t.Fatalf("expected second dequeue t3, got %+v", second)
	}
	if session.HasPendingTurns() {
		t.Fatal("expected pending queue empty")
	}
}

func TestPendingTurnQueueConcurrentAccess(t *testing.T) {
	session := newTestSession()
	session.maxPendingTurns = 8

	var waitGroup sync.WaitGroup
	for index := 0; index < 20; index++ {
		waitGroup.Add(1)
		go func(iteration int) {
			defer waitGroup.Done()
			session.EnqueuePendingTurn("turn", "text")
			if iteration%2 == 0 {
				session.DequeuePendingTurn()
			}
			session.HasPendingTurns()
		}(index)
	}
	waitGroup.Wait()
}
