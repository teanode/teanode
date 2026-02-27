package voice

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/deferutil"
)

var pipelineLog = logging.MustGetLogger("voice.pipeline")

const (
	minCommittedTurnBytes  = 6400 // ~200ms at 16kHz mono s16le
	minCommittedTextRunes  = 1
	bargeInTriggerMinScore = 0.06
	maxResponseStartDelay  = 2 * time.Second
	vadPreRollFrames       = 8 // keep ~160ms leading context so first words aren't clipped
)

const voiceCallPromptSuffix = "The user is in a live voice call with you. Their messages are transcribed speech and your responses will be spoken aloud in real time. Keep responses brief and conversational - 1-3 sentences unless the user asks for more detail. Avoid markdown formatting, code blocks, and bullet lists."

func (self *Session) audioInputLoop() {
	vad := &VADState{}
	var speechBuf []byte
	preSpeech := make([][]byte, 0, vadPreRollFrames)

	for {
		select {
		case <-self.doneCh:
			return
		case frame := <-self.audioInCh:
			if !vad.IsSpeaking {
				frameCopy := append([]byte(nil), frame...)
				preSpeech = append(preSpeech, frameCopy)
				if len(preSpeech) > vadPreRollFrames {
					preSpeech = preSpeech[1:]
				}
			}
			started, ended, score := vad.ProcessFrame(frame)
			if started {
				turnId := self.newTurnId()
				self.startNewTurn(turnId)
				speechBuf = speechBuf[:0]
				for _, buffered := range preSpeech {
					speechBuf = append(speechBuf, buffered...)
				}
				preSpeech = preSpeech[:0]
				pipelineLog.Infof("voice speech_started: session=%s turn=%s seq_ref=%d score=%.4f", self.ID, turnId, self.inSeq.Load(), score)
				self.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnId,
					Event:       "speech_started",
					VADScore:    score,
					AudioSeqRef: self.inSeq.Load(),
				})
				if self.Features.BargeIn &&
					score >= bargeInTriggerMinScore &&
					(self.GetCurrentRunID() != "" || self.GetCurrentResponseID() != "") {
					self.triggerBargeIn()
				}
			}

			if vad.IsSpeaking {
				// Current frame is already included when speech just started via pre-roll.
				if !started {
					speechBuf = append(speechBuf, frame...)
				}
			}

			if ended {
				turnId := self.GetCurrentTurnID()
				pipelineLog.Infof("voice speech_ended: session=%s turn=%s bytes=%d seq_ref=%d score=%.4f", self.ID, self.GetCurrentTurnID(), len(speechBuf), self.inSeq.Load(), score)
				self.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnId,
					Event:       "speech_ended",
					VADScore:    score,
					AudioSeqRef: self.inSeq.Load(),
				})
				captured := append([]byte(nil), speechBuf...)
				speechBuf = speechBuf[:0]
				if len(captured) < minCommittedTurnBytes {
					pipelineLog.Infof("voice turn ignored (too short): session=%s turn=%s bytes=%d", self.ID, turnId, len(captured))
					self.sendVoiceEvent("turn.event", turnEventPayload{
						TurnID: turnId,
						Event:  "turn_dropped",
						Reason: "dropped_too_short_audio",
					})
					continue
				}
				if !self.TryStartTurnTranscription(turnId) {
					pipelineLog.Infof("voice turn transcription skipped (duplicate): session=%s turn=%s", self.ID, turnId)
					continue
				}
				go func(turnId string, audio []byte) {
					defer deferutil.Recover()
					defer self.FinishTurnTranscription(turnId)
					self.transcribeAndSend(turnId, audio)
				}(turnId, captured)
			}
		}
	}
}

func (self *Session) llmEventForwarder() {
	if self.pubsub == nil {
		return
	}
	subscriber := &conversationEventSubscriber{
		conversationId: self.ConversationID,
		eventCh:        make(chan map[string]interface{}, 128),
	}
	self.pubsub.Subscribe(subscriber)
	defer self.pubsub.Unsubscribe(subscriber)

	streamText := ""
	sentencesEnqueued := 0
	sawDelta := false

	for {
		select {
		case <-self.doneCh:
			return
		case event := <-subscriber.eventCh:
			state, _ := event["state"].(string)
			text, _ := event["text"].(string)
			runId, _ := event["runId"].(string)
			if runId != "" && self.IsRunCanceled(runId) {
				if state == "final" || state == "aborted" || state == "error" {
					self.ClearCanceledRun(runId)
				}
				continue
			}
			if runId != "" && (state == "queued" || state == "delta") {
				self.SetCurrentRunID(runId)
			}
			if state == "queued" || state == "final" || state == "error" || state == "aborted" {
				pipelineLog.Debugf("voice llm event: session=%s turn=%s state=%s text_len=%d run=%s", self.ID, self.GetCurrentTurnID(), state, len(text), self.GetCurrentRunID())
			}
			if state == "delta" {
				if text != "" {
					streamText += text
					sawDelta = true
				}
				newSentences, nextCount := ExtractCompleteSentences(streamText, sentencesEnqueued)
				sentencesEnqueued = nextCount
				if len(newSentences) > 0 {
					pipelineLog.Debugf("voice sentence enqueue: session=%s count=%d total=%d", self.ID, len(newSentences), sentencesEnqueued)
				}
				for _, sentence := range newSentences {
					select {
					case self.ttsInCh <- sentence:
					case <-self.doneCh:
						return
					}
				}
			}
			if state == "final" || state == "aborted" || state == "error" {
				streamForFlush := streamText
				// Some providers may only emit final text (no deltas). In that case,
				// use the final text as the source for sentence flushing.
				if !sawDelta && text != "" {
					streamForFlush = text
				}
				remaining := FlushRemaining(streamForFlush, sentencesEnqueued)
				if remaining != "" {
					select {
					case self.ttsInCh <- remaining:
					case <-self.doneCh:
						return
					}
				}
				select {
				case self.ttsInCh <- "":
				case <-self.doneCh:
					return
				}
				// Response stream is complete; allow next transcript to commit a new run.
				self.ClearCurrentRun()
				self.ClearCanceledRun(runId)
				streamText = ""
				sentencesEnqueued = 0
				sawDelta = false
				self.commitNextPendingTurn()
			}
		}
	}
}

func (self *Session) ttsSynthLoop() {
	for {
		select {
		case <-self.doneCh:
			return
		case sentence := <-self.ttsInCh:
			if sentence == "" {
				pipelineLog.Infof("voice response completed: session=%s response=%s", self.ID, self.GetCurrentResponseID())
				if responseId := self.GetCurrentResponseID(); responseId != "" {
					self.sendVoiceEvent("response.completed", map[string]interface{}{
						"response_id": responseId,
						"turn_id":     self.GetCurrentTurnID(),
					})
				}
				self.ClearCurrentResponse()
				continue
			}
			if self.dispatcher == nil {
				pipelineLog.Warningf("voice synthesis skipped: missing coordinator")
				continue
			}
			synth, _, ok := self.dispatcher.ProviderRegistry().FindSynthesizer()
			if !ok || synth == nil {
				pipelineLog.Warningf("voice synthesis skipped: no synthesizer configured")
				continue
			}
			responseId := self.GetCurrentResponseID()
			if responseId == "" {
				// Avoid speaking between two close user utterances while a transcription
				// is still in-flight for a newer turn.
				start := time.Now()
				for self.HasTranscriptionInFlight() && time.Since(start) < maxResponseStartDelay {
					select {
					case <-self.doneCh:
						return
					case <-time.After(50 * time.Millisecond):
					}
				}
				responseId = self.newTurnId()
				self.SetCurrentResponseID(responseId)
				pipelineLog.Infof("voice response started: session=%s response=%s turn=%s", self.ID, responseId, self.GetCurrentTurnID())
				self.sendVoiceEvent("response.started", map[string]interface{}{
					"response_id": responseId,
					"turn_id":     self.GetCurrentTurnID(),
				})
			}

			ttsCtx, cancel := context.WithCancel(context.Background())
			prev := self.SwapTTSCancel(cancel)
			if prev != nil {
				prev()
			}
			pipelineLog.Infof("voice tts input: session=%s response=%s turn=%s text_len=%d text=%q", self.ID, self.GetCurrentResponseID(), self.GetCurrentTurnID(), len(sentence), sentence)
			audio, err := synthesizePCM(ttsCtx, synth, sentence, "alloy", self.AudioOut.SampleRateHz)
			self.SwapTTSCancel(nil)
			if err != nil {
				if ttsCtx.Err() != nil {
					continue
				}
				pipelineLog.Warningf("voice synthesis failed: %v", err)
				continue
			}
			pipelineLog.Debugf("voice tts bytes: session=%s response=%s sentence_len=%d bytes=%d", self.ID, self.GetCurrentResponseID(), len(sentence), len(audio))
			payload := EncodeBinaryAudioFrame(BinaryAudioFrame{
				FrameType:   FrameTypeAudioOut,
				Seq:         self.NextOutSeq(),
				CaptureTSMs: time.Now().UnixMilli(),
				DurationMS:  0,
				Data:        audio,
			})
			self.enqueueAudioOut(payload)
		}
	}
}

func (self *Session) audioOutputLoop() {
	for {
		select {
		case <-self.doneCh:
			return
		case data := <-self.audioOutCh:
			if self.sendBinaryFn != nil {
				self.sendBinaryFn(data)
			}
		}
	}
}

func (self *Session) transcribeAndSend(turnId string, captured []byte) {
	if len(captured) == 0 {
		return
	}
	if self.dispatcher == nil {
		pipelineLog.Warningf("voice transcription skipped: missing coordinator")
		return
	}
	pipelineLog.Infof("voice transcribe start: session=%s turn=%s bytes=%d", self.ID, turnId, len(captured))
	transcriber, _, ok := self.dispatcher.ProviderRegistry().FindTranscriber()
	if !ok || transcriber == nil {
		pipelineLog.Warningf("voice transcription skipped: no transcriber configured")
		return
	}

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
	if text == "" {
		pipelineLog.Infof("voice transcript ignored (empty): session=%s turn=%s", self.ID, turnId)
		self.sendVoiceEvent("turn.event", turnEventPayload{
			TurnID: turnId,
			Event:  "turn_dropped",
			Reason: "dropped_empty_transcript",
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
		return
	}
	// If an older response already started speaking, interrupt it and prioritize this newer user turn.
	if self.GetCurrentResponseID() != "" {
		self.triggerBargeIn()
	}
	if self.GetCurrentRunID() != "" {
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
	pipelineLog.Infof("voice transcript.final: session=%s turn=%s text_len=%d text=%q", self.ID, turnId, len(text), text)
	self.SetLastCommittedTranscript(text)
	self.sendVoiceEvent("transcript.final", map[string]interface{}{
		"turn_id": turnId,
		"text":    text,
	})
	handle, err := self.dispatcher.Run(context.Background(), coordinators.RunParameters{
		AgentID:            self.AgentID,
		ConversationID:     self.ConversationID,
		Message:            text,
		SystemPromptSuffix: self.effectivePromptSuffix(),
		Origin:             "voice",
	}, nil)
	self.MarkTurnCommitted(turnId)
	if err != nil {
		pipelineLog.Warningf("voice turn commit error: session=%s turn=%s err=%v", self.ID, turnId, err)
		return
	}
	self.SetCurrentRunID(handle.RunID)
	pipelineLog.Infof("voice turn committed: session=%s turn=%s run=%s", self.ID, turnId, handle.RunID)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnId,
		Event:  "turn_committed",
	})
}

func (self *Session) effectivePromptSuffix() string {
	if self.PromptSuffix != "" {
		return self.PromptSuffix
	}
	return voiceCallPromptSuffix
}

func (self *Session) transcriptionPrompt() string {
	// Keep STT unprompted to avoid model-side semantic "helpfulness" that can
	// drift from literal user speech.
	return ""
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
	}
	pipelineLog.Infof("voice turn queued (run active): session=%s turn=%s run=%s depth=%d", self.ID, turnId, self.GetCurrentRunID(), depth)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID:     turnId,
		Event:      "turn_queued",
		Reason:     "queued_run_active",
		QueueDepth: depth,
	})
}

func (self *Session) commitNextPendingTurn() {
	if self.GetCurrentRunID() != "" {
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

func (self *Session) triggerBargeIn() {
	self.bargeInOnce.Do(func() {
		pipelineLog.Infof("voice barge_in triggered: session=%s run=%s response=%s", self.ID, self.GetCurrentRunID(), self.GetCurrentResponseID())
		runId := self.GetCurrentRunID()
		self.MarkRunCanceled(runId)
		if prev := self.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		self.drainTTSQueue()
		self.drainAudioOutQueue()
		self.trySendFlushFrame()
		if runId != "" && self.dispatcher != nil {
			self.dispatcher.AbortRun(runId)
		}
		self.ClearCurrentRun()
		self.ClearCurrentResponse()
		self.sendVoiceEvent("turn.event", turnEventPayload{Event: "barge_in_triggered"})
	})
}

func (self *Session) drainTTSQueue() {
	for {
		select {
		case <-self.ttsInCh:
		default:
			return
		}
	}
}

func (self *Session) drainAudioOutQueue() {
	for {
		select {
		case <-self.audioOutCh:
		default:
			return
		}
	}
}

func (self *Session) startNewTurn(turnId string) {
	self.stateMu.Lock()
	self.currentTurnId = turnId
	self.bargeInOnce = sync.Once{}
	self.stateMu.Unlock()
}

func (self *Session) trySendFlushFrame() {
	pipelineLog.Debugf("voice flush frame queued: session=%s", self.ID)
	payload := EncodeBinaryAudioFrame(BinaryAudioFrame{
		FrameType:   FrameTypeFlush,
		Seq:         self.NextOutSeq(),
		CaptureTSMs: time.Now().UnixMilli(),
		DurationMS:  0,
	})
	self.enqueueAudioOut(payload)
}
