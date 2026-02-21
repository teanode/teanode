package voice

import (
	"context"
	"sync"
	"time"

	"github.com/op/go-logging"
)

var pipelineLog = logging.MustGetLogger("voice.pipeline")

func (s *Session) audioInputLoop() {
	for {
		select {
		case <-s.doneCh:
			return
		case <-s.audioInCh:
			// Implemented in A6.
		}
	}
}

func (s *Session) llmEventForwarder() {
	for {
		select {
		case <-s.doneCh:
			return
		case <-time.After(250 * time.Millisecond):
			// Implemented in A6.
		}
	}
}

func (s *Session) ttsSynthLoop() {
	for {
		select {
		case <-s.doneCh:
			return
		case <-s.ttsInCh:
			// Implemented in A6.
		}
	}
}

func (s *Session) audioOutputLoop() {
	for {
		select {
		case <-s.doneCh:
			return
		case data := <-s.audioOutCh:
			if s.sendBinaryFn != nil {
				s.sendBinaryFn(data)
			}
		}
	}
}

func (s *Session) transcribeAndSend(_ []byte) {
	// Implemented in A6.
}

func (s *Session) triggerBargeIn() {
	s.bargeInOnce.Do(func() {
		if prev := s.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		if runID := s.GetCurrentRunID(); runID != "" && s.deps != nil {
			s.deps.AbortRun(runID)
		}
		s.ClearCurrentRun()
		s.ClearCurrentResponse()
		s.sendVoiceEvent("turn.event", turnEventPayload{Event: "barge_in_triggered"})
	})
}

func (s *Session) startNewTurn(turnID string) {
	s.stateMu.Lock()
	s.currentTurnID = turnID
	s.bargeInOnce = sync.Once{}
	s.stateMu.Unlock()
}

func (s *Session) startRun(ctx context.Context, text string) {
	_ = ctx
	_ = text
	// Implemented in A6.
}
