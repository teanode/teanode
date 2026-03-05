package voice

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/providers"
)

func (self *Session) commitCapturedTurn(turnId string, captured []byte) {
	if self.getStreamingTranscribeStream() != nil {
		if !self.TryStartTurnTranscription(turnId) {
			pipelineLog.Infof("voice turn transcription skipped (duplicate): session=%s turn=%s", self.ID, turnId)
			return
		}
		self.transcriptionWg.Add(1)
		go func(tid string, audio []byte) {
			defer self.transcriptionWg.Done()
			defer self.FinishTurnTranscription(tid)
			deadline := time.Now().Add(streamingFinalGracePeriod)
			for time.Now().Before(deadline) {
				if self.IsTurnCommitted(tid) {
					return
				}
				select {
				case <-self.doneCh:
					return
				case <-time.After(25 * time.Millisecond):
				}
			}
			if self.IsTurnCommitted(tid) {
				return
			}
			finalText := strings.TrimSpace(self.takeStreamingFinalText(tid))
			if finalText != "" && len([]rune(finalText)) >= minStreamingFinalRunes {
				self.transcribeTextAndSend(tid, finalText)
				return
			}
			self.transcribeAndSend(tid, audio)
		}(turnId, captured)
		return
	}
	if !self.TryStartTurnTranscription(turnId) {
		pipelineLog.Infof("voice turn transcription skipped (duplicate): session=%s turn=%s", self.ID, turnId)
		return
	}
	self.transcriptionWg.Add(1)
	go func(tid string, audio []byte) {
		defer self.transcriptionWg.Done()
		defer self.FinishTurnTranscription(tid)
		self.transcribeAndSend(tid, audio)
	}(turnId, captured)
}

func (self *Session) transcribeAndSend(turnId string, captured []byte) {
	if len(captured) == 0 {
		return
	}
	if self.dispatcher == nil {
		pipelineLog.Warningf("voice transcription skipped: missing dispatcher")
		return
	}
	if self.dispatcher.ProviderRegistry() == nil {
		pipelineLog.Warningf("voice transcription skipped: provider registry unavailable")
		return
	}
	transcriber, transcriberProvider, ok := self.dispatcher.ProviderRegistry().FindTranscriber()
	if !ok || transcriber == nil {
		pipelineLog.Warningf("voice transcription skipped: no transcriber configured")
		return
	}
	pipelineLog.Infof("voice transcribe start: session=%s turn=%s bytes=%d provider=%s model=%s", self.ID, turnId, len(captured), transcriberProvider, voiceProviderModelHint("transcriber", transcriberProvider))

	wav := PCMToWAV(captured, self.AudioIn.SampleRateHz, self.AudioIn.Channels)
	result, err := transcriber.Transcribe(context.Background(), providers.TranscribeRequest{
		Audio:    bytes.NewReader(wav),
		Format:   "wav",
		Language: "",
		Prompt:   self.transcriptionPrompt(),
	})
	if err != nil || result == nil {
		if err != nil {
			pipelineLog.Warningf("voice transcription failed: %v", err)
		} else {
			pipelineLog.Warningf("voice transcription failed: empty result")
		}
		return
	}
	text := strings.TrimSpace(result.Text)
	self.transcribeTextAndSend(turnId, text)
}

func (self *Session) transcribeTextAndSend(turnId, rawText string) {
	text := strings.TrimSpace(rawText)
	if text == "" {
		pipelineLog.Infof("voice transcript ignored (empty): session=%s turn=%s", self.ID, turnId)
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turn_dropped",
			Reason: "dropped_empty_transcript",
		})
		self.notifyObservers(func(observer TurnObserver) {
			observer.OnTurnDropped(turnId, "dropped_empty_transcript", time.Now().UnixMilli())
		})
		return
	}
	if len([]rune(text)) < minCommittedTextRunes {
		pipelineLog.Infof("voice transcript ignored (too short): session=%s turn=%s text=%q", self.ID, turnId, text)
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turn_dropped",
			Reason: "dropped_too_short_text",
		})
		self.notifyObservers(func(observer TurnObserver) {
			observer.OnTurnDropped(turnId, "dropped_too_short_text", time.Now().UnixMilli())
		})
		return
	}
	// If an older response already started speaking, interrupt it and prioritize this newer user turn.
	if self.ResponseIsActive() {
		self.triggerBargeIn()
	}
	if self.RunIsActive() {
		self.enqueueTranscriptTurn(turnId, text)
		return
	}
	if self.IsTurnCommitted(turnId) {
		pipelineLog.Infof("voice transcript ignored (already committed): session=%s turn=%s", self.ID, turnId)
		return
	}
	self.commitVoiceTurn(turnId, text)
}

func (self *Session) commitVoiceTurn(turnId, text string) {
	nowMs := time.Now().UnixMilli()
	pipelineLog.Infof("voice transcript.final: session=%s turn=%s text_len=%d text=%q", self.ID, turnId, len(text), text)
	self.SetLastCommittedTranscript(text)
	self.sendVoiceEvent("transcript.final", map[string]interface{}{
		"turn_id": turnId,
		"text":    text,
	})
	self.notifyObservers(func(observer TurnObserver) {
		observer.OnTranscriptFinal(turnId, nowMs)
	})
	handle, err := self.dispatcher.Run(context.Background(), coordinators.RunParameters{
		AgentID:            self.AgentID,
		ConversationID:     self.ConversationID,
		Message:            text,
		SystemPromptSuffix: self.effectivePromptSuffix(),
		Origin:             "voice",
	}, nil)
	if err != nil {
		pipelineLog.Warningf("voice commitVoiceTurn Run error: %v", err)
		return
	}
	self.MarkTurnCommitted(turnId)
	self.SetCurrentRunId(handle.RunID)
	self.MapRunToTurn(handle.RunID, turnId)
	pipelineLog.Infof("voice turn committed: session=%s turn=%s run=%s", self.ID, turnId, handle.RunID)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnId,
		Event:  "turn_committed",
	})
	self.notifyObservers(func(observer TurnObserver) {
		observer.OnTurnCommitted(turnId, time.Now().UnixMilli())
	})
}

func (self *Session) enqueueTranscriptTurn(turnId, text string) {
	dropped, depth := self.EnqueuePendingTurn(turnId, text)
	if dropped != nil {
		pipelineLog.Infof("voice turn dropped (queue overflow): session=%s dropped_turn=%s", self.ID, dropped.TurnID)
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID:     dropped.TurnID,
			Event:      "turn_dropped",
			Reason:     "dropped_queue_overflow",
			QueueDepth: depth,
		})
		self.notifyObservers(func(observer TurnObserver) {
			observer.OnTurnDropped(dropped.TurnID, "dropped_queue_overflow", time.Now().UnixMilli())
		})
	}
	pipelineLog.Infof("voice turn queued (run active): session=%s turn=%s run=%s depth=%d", self.ID, turnId, self.GetCurrentRunId(), depth)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID:     turnId,
		Event:      "turn_queued",
		Reason:     "queued_run_active",
		QueueDepth: depth,
	})
}

func (self *Session) commitNextPendingTurn() {
	if self.RunIsActive() {
		return
	}
	next, ok := self.DequeuePendingTurn()
	if !ok {
		return
	}
	if next.Text == "" || next.TurnID == "" || self.IsTurnCommitted(next.TurnID) {
		return
	}
	pipelineLog.Infof("voice queued turn draining: session=%s turn=%s", self.ID, next.TurnID)
	self.commitVoiceTurn(next.TurnID, next.Text)
}
