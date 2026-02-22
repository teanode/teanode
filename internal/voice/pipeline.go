package voice

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
)

var pipelineLog = logging.MustGetLogger("voice.pipeline")

const (
	minCommittedTurnBytes  = 19200 // ~600ms at 16kHz mono s16le
	minCommittedTextRunes  = 8
	bargeInTriggerMinScore = 0.1
	maxResponseStartDelay  = 2 * time.Second
)

const voiceCallPromptSuffix = "The user is in a live voice call with you. Their messages are transcribed speech and your responses will be spoken aloud in real time. Keep responses brief and conversational - 1-3 sentences unless the user asks for more detail. Avoid markdown formatting, code blocks, and bullet lists."

func (self *Session) audioInputLoop() {
	vad := &VADState{}
	var speechBuf []byte

	for {
		select {
		case <-self.doneCh:
			return
		case frame := <-self.audioInCh:
			started, ended, score := vad.ProcessFrame(frame)
			if started {
				turnId := self.newTurnId()
				self.startNewTurn(turnId)
				pipelineLog.Infof("voice speech_started: session=%s turn=%s seq_ref=%d score=%.4f", self.ID, turnId, self.inSeq.Load(), score)
				self.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnId,
					Event:       "speech_started",
					VADScore:    score,
					AudioSeqRef: self.inSeq.Load(),
				})
				if self.Features.BargeIn && score >= bargeInTriggerMinScore && (self.GetCurrentRunId() != "" || self.GetCurrentResponseId() != "") {
					self.triggerBargeIn()
				}
			}

			if vad.IsSpeaking {
				speechBuf = append(speechBuf, frame...)
			}

			if ended {
				turnId := self.GetCurrentTurnId()
				pipelineLog.Infof("voice speech_ended: session=%s turn=%s bytes=%d seq_ref=%d score=%.4f", self.ID, self.GetCurrentTurnId(), len(speechBuf), self.inSeq.Load(), score)
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
				go func(tid string, audio []byte) {
					defer self.FinishTurnTranscription(tid)
					self.transcribeAndSend(tid, audio)
				}(turnId, captured)
			}
		}
	}
}

func (self *Session) llmEventForwarder() {
	if self.deps == nil {
		return
	}
	sub := &conversationEventSubscriber{
		conversationId: self.ConversationID,
		eventCh:        make(chan map[string]interface{}, 128),
	}
	self.deps.Subscribe(sub)
	defer self.deps.Unsubscribe(sub)

	streamText := ""
	sentencesEnqueued := 0
	sawDelta := false

	for {
		select {
		case <-self.doneCh:
			return
		case event := <-sub.eventCh:
			state, _ := event["state"].(string)
			text, _ := event["text"].(string)
			if runId, _ := event["runId"].(string); runId != "" && (state == "queued" || state == "delta") {
				self.SetCurrentRunId(runId)
			}
			if state == "queued" || state == "final" || state == "error" || state == "aborted" {
				pipelineLog.Debugf("voice llm event: session=%s turn=%s state=%s text_len=%d run=%s", self.ID, self.GetCurrentTurnId(), state, len(text), self.GetCurrentRunId())
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
				if !sawDelta && strings.TrimSpace(text) != "" {
					streamForFlush = text
				}
				remaining := strings.TrimSpace(FlushRemaining(streamForFlush, sentencesEnqueued))
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
				pipelineLog.Infof("voice response completed: session=%s response=%s", self.ID, self.GetCurrentResponseId())
				if rid := self.GetCurrentResponseId(); rid != "" {
					self.sendVoiceEvent("response.completed", map[string]interface{}{
						"response_id": rid,
						"turn_id":     self.GetCurrentTurnId(),
					})
				}
				self.ClearCurrentResponse()
				continue
			}
			if self.deps == nil {
				pipelineLog.Warningf("voice synthesis skipped: missing gateway deps")
				continue
			}
			if self.deps.ProviderRegistry() == nil {
				pipelineLog.Warningf("voice synthesis skipped: provider registry unavailable")
				continue
			}
			synth, _, ok := self.deps.ProviderRegistry().FindSynthesizer()
			if !ok || synth == nil {
				pipelineLog.Warningf("voice synthesis skipped: no synthesizer configured")
				continue
			}
			responseId := self.GetCurrentResponseId()
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
				self.SetCurrentResponseId(responseId)
				pipelineLog.Infof("voice response started: session=%s response=%s turn=%s", self.ID, responseId, self.GetCurrentTurnId())
				self.sendVoiceEvent("response.started", map[string]interface{}{
					"response_id": responseId,
					"turn_id":     self.GetCurrentTurnId(),
				})
			}

			ttsCtx, cancel := context.WithCancel(context.Background())
			prev := self.SwapTTSCancel(cancel)
			if prev != nil {
				prev()
			}
			pipelineLog.Infof("voice tts input: session=%s response=%s turn=%s text_len=%d text=%q", self.ID, self.GetCurrentResponseId(), self.GetCurrentTurnId(), len(sentence), sentence)
			audio, err := synth.SynthesizePCM(ttsCtx, sentence, "alloy", self.AudioOut.SampleRateHz)
			self.SwapTTSCancel(nil)
			if err != nil {
				if ttsCtx.Err() != nil {
					continue
				}
				pipelineLog.Warningf("voice synthesis failed: %v", err)
				continue
			}
			pipelineLog.Debugf("voice tts bytes: session=%s response=%s sentence_len=%d bytes=%d", self.ID, self.GetCurrentResponseId(), len(sentence), len(audio))
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
	if self.deps == nil {
		pipelineLog.Warningf("voice transcription skipped: missing gateway deps")
		return
	}
	if self.deps.ProviderRegistry() == nil {
		pipelineLog.Warningf("voice transcription skipped: provider registry unavailable")
		return
	}
	pipelineLog.Infof("voice transcribe start: session=%s turn=%s bytes=%d", self.ID, turnId, len(captured))
	transcriber, _, ok := self.deps.ProviderRegistry().FindTranscriber()
	if !ok || transcriber == nil {
		pipelineLog.Warningf("voice transcription skipped: no transcriber configured")
		return
	}

	wav := PCMToWAV(captured, self.AudioIn.SampleRateHz, self.AudioIn.Channels)
	result, err := transcriber.Transcribe(context.Background(), VoiceTranscribeRequest{
		Audio:      wav,
		Format:     "wav",
		SampleRate: self.AudioIn.SampleRateHz,
		Channels:   self.AudioIn.Channels,
		Prompt:     self.transcriptionPrompt(),
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
	if self.GetCurrentResponseId() != "" {
		self.triggerBargeIn()
	}
	if self.GetCurrentRunId() != "" {
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
	run := self.deps.SendMessage(context.Background(), VoiceSendMessageParams{
		AgentID:            self.AgentID,
		ConversationID:     self.ConversationID,
		Message:            text,
		SystemPromptSuffix: self.effectivePromptSuffix(),
	})
	self.MarkTurnCommitted(turnId)
	self.SetCurrentRunId(run.RunID)
	pipelineLog.Infof("voice turn committed: session=%s turn=%s run=%s", self.ID, turnId, run.RunID)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnId,
		Event:  "turn_committed",
	})
}

func (self *Session) effectivePromptSuffix() string {
	if strings.TrimSpace(self.PromptSuffix) != "" {
		return self.PromptSuffix
	}
	return voiceCallPromptSuffix
}

func (self *Session) transcriptionPrompt() string {
	last := strings.TrimSpace(self.GetLastCommittedTranscript())
	base := "This is a live voice conversation transcription. Transcribe literally and preserve question words and place names exactly."
	if last == "" {
		return base
	}
	return base + " Prior user utterance context: " + last
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
	pipelineLog.Infof("voice turn queued (run active): session=%s turn=%s run=%s depth=%d", self.ID, turnId, self.GetCurrentRunId(), depth)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID:     turnId,
		Event:      "turn_queued",
		Reason:     "queued_run_active",
		QueueDepth: depth,
	})
}

func (self *Session) commitNextPendingTurn() {
	if self.GetCurrentRunId() != "" {
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
		pipelineLog.Infof("voice barge_in triggered: session=%s run=%s response=%s", self.ID, self.GetCurrentRunId(), self.GetCurrentResponseId())
		if prev := self.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		self.trySendFlushFrame()
		if runId := self.GetCurrentRunId(); runId != "" && self.deps != nil {
			self.deps.AbortRun(runId)
		}
		self.ClearCurrentRun()
		self.ClearCurrentResponse()
		self.sendVoiceEvent("turn.event", turnEventPayload{Event: "barge_in_triggered"})
	})
}

func (self *Session) startNewTurn(turnId string) {
	self.stateMu.Lock()
	self.currentTurnId = turnId
	self.bargeInOnce = sync.Once{}
	self.stateMu.Unlock()
}

func (self *Session) startRun(ctx context.Context, text string) {
	_ = ctx
	_ = text
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

type conversationEventSubscriber struct {
	conversationId string
	eventCh        chan map[string]interface{}
}

func (self *conversationEventSubscriber) OnVoiceEvent(eventType string, payload interface{}) {
	if eventType != "conversation" {
		return
	}
	eventMap, ok := payload.(map[string]interface{})
	if !ok {
		return
	}
	conversationId, _ := eventMap["conversationId"].(string)
	if conversationId != self.conversationId {
		return
	}
	state, _ := eventMap["state"].(string)
	critical := state == "final" || state == "error" || state == "aborted" || state == "queued"
	if !critical {
		select {
		case self.eventCh <- eventMap:
		default:
		}
		return
	}

	select {
	case self.eventCh <- eventMap:
	default:
		// Preserve terminal lifecycle events by making room if queue is saturated by deltas.
		select {
		case <-self.eventCh:
		default:
		}
		select {
		case self.eventCh <- eventMap:
		default:
		}
	}
}
