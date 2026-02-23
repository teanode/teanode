package voice

import (
	"sync"
	"testing"
)

func TestMetricsObserver_AllFieldsSet(t *testing.T) {
	var captured TurnMetrics
	observer := NewMetricsObserver(func(metric TurnMetrics) {
		captured = metric
	})

	observer.OnSpeechStarted("turn-1", 1000)
	observer.OnSpeechEnded("turn-1", 1200)
	observer.OnTranscriptFinal("turn-1", 1320)
	observer.OnTurnCommitted("turn-1", 1330)
	observer.OnTTSRequested("turn-1", 1470)
	observer.OnResponseStarted("turn-1", "resp-1", 1500)

	if captured.TurnID != "turn-1" {
		t.Fatalf("unexpected turn id %q", captured.TurnID)
	}
	if captured.ResponseID != "resp-1" {
		t.Fatalf("unexpected response id %q", captured.ResponseID)
	}
	if captured.SpeechStartedMS == 0 || captured.SpeechEndedMS == 0 || captured.TranscriptFinalMS == 0 || captured.TurnCommittedMS == 0 || captured.ResponseStartedMS == 0 {
		t.Fatalf("expected all timestamps to be set: %+v", captured)
	}
	if captured.STTMS != 120 {
		t.Fatalf("unexpected stt_ms: %d", captured.STTMS)
	}
	if captured.LLMTTFBMS != 170 {
		t.Fatalf("unexpected llm_ttfb_ms: %d", captured.LLMTTFBMS)
	}
	if captured.TTSMS != 30 {
		t.Fatalf("unexpected tts_ms: %d", captured.TTSMS)
	}
	if captured.E2EMS != 300 {
		t.Fatalf("unexpected e2e_ms: %d", captured.E2EMS)
	}
}

func TestNotifyObservers_EmptyList(t *testing.T) {
	session := &Session{}
	session.notifyObservers(func(observer TurnObserver) {
		t.Fatal("callback should not run when observer list is empty")
	})
}

func TestMetricsObserver_ThreadSafe(t *testing.T) {
	observer := NewMetricsObserver(nil)
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for i := int64(0); i < 500; i++ {
			observer.OnSpeechStarted("turn-1", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := int64(0); i < 500; i++ {
			observer.OnSpeechEnded("turn-1", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := int64(0); i < 500; i++ {
			observer.OnTranscriptFinal("turn-1", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := int64(0); i < 500; i++ {
			observer.OnTurnCommitted("turn-1", i)
		}
	}()

	wg.Wait()
}
