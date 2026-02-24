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
	minCommittedTurnBytes     = 6400 // ~200ms at 16kHz mono s16le
	minCommittedTextRunes     = 1
	bargeInTriggerMinScore    = 0.06
	maxResponseStartDelay     = 2 * time.Second
	vadPreRollFrames          = 8 // keep ~160ms leading context so first words aren't clipped
	streamingFinalGracePeriod = 75 * time.Millisecond
	// voiceMaxContextTokens is the estimated-token budget for voice LLM requests.
	// Uses len(text)/4 approximation. Keeps recent turns within a 16k window so
	// long sessions do not balloon the prompt beyond model context limits.
	voiceMaxContextTokens = 16000
)

const voiceCallPromptSuffix = "The user is in a live voice call with you. Their messages are transcribed speech and your responses will be spoken aloud in real time. Keep responses brief and conversational - 1-3 sentences unless the user asks for more detail. Avoid markdown formatting, code blocks, and bullet lists."

func voiceProviderModelHint(kind, provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai":
		if kind == "synthesizer" {
			return "tts-1"
		}
		return "whisper-1"
	case "deepgram":
		return "nova-2"
	case "elevenlabs":
		return "eleven_flash_v2_5"
	default:
		return "unknown"
	}
}

func (self *Session) audioInputLoop() {
	vad := VADAnalyzer(&EnergyVAD{})
	var speechBuf []byte
	preSpeech := make([][]byte, 0, vadPreRollFrames)
	speaking := false
	candidateActive := false
	pendingCommitTurnID := ""
	var pendingCommitAudio []byte
	pendingSilenceMs := 0
	frameDurationMs := self.AudioIn.FrameMS
	if frameDurationMs <= 0 {
		frameDurationMs = 20
	}

	for {
		select {
		case <-self.doneCh:
			return
		case frame := <-self.audioInCh:
			var preRollFrames [][]byte
			if !self.Features.ServerVAD {
				self.accumulateExplicitAudio(frame)
				continue
			}
			if pendingCommitTurnID != "" {
				pendingSilenceMs += frameDurationMs
				if self.strategy.ShouldCommitTurn(TurnContext{
					SilenceDurationMs: pendingSilenceMs,
					InterimText:       self.getInterimText(),
				}) {
					turnId := pendingCommitTurnID
					captured := append([]byte(nil), pendingCommitAudio...)
					pendingCommitTurnID = ""
					pendingCommitAudio = nil
					pendingSilenceMs = 0
					self.commitCapturedTurn(turnId, captured)
				}
			}
			if !speaking {
				cp := append([]byte(nil), frame...)
				preSpeech = append(preSpeech, cp)
				if len(preSpeech) > vadPreRollFrames {
					preSpeech = preSpeech[1:]
				}
			}
			started, ended, score := vad.ProcessFrame(frame)
			if started {
				speaking = true
				self.setUserSpeaking(true)
				turnId := self.newTurnId()
				self.startNewTurn(turnId)
				_, balanced := self.strategy.(BalancedStrategy)
				if balanced && self.Features.BargeIn && (self.GetCurrentRunId() != "" || self.GetCurrentResponseId() != "") {
					self.triggerBargeIn()
				}
				self.setSpeechStartedAt(time.Now())
				candidateActive = false
				nowMs := time.Now().UnixMilli()
				speechBuf = speechBuf[:0]
				for _, buffered := range preSpeech {
					speechBuf = append(speechBuf, buffered...)
				}
				preRollFrames = append(preRollFrames, preSpeech...)
				preSpeech = preSpeech[:0]
				pipelineLog.Infof("voice speech_started: session=%s turn=%s seq_ref=%d score=%.4f", self.ID, turnId, self.inSeq.Load(), score)
				self.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnId,
					Event:       "speech_started",
					VADScore:    score,
					AudioSeqRef: self.inSeq.Load(),
				})
				self.notifyObservers(func(observer TurnObserver) {
					observer.OnSpeechStarted(turnId, nowMs)
				})
			}

			if speaking {
				// Current frame is already included when speech just started via pre-roll.
				if !started {
					speechBuf = append(speechBuf, frame...)
				}
				if stream := self.getStreamingTranscribeStream(); stream != nil {
					sendStreamFrame := func(data []byte) bool {
						if err := stream.SendAudio(data); err != nil {
							pipelineLog.Warningf("voice streaming stt send failed, falling back to batch: %v", err)
							_ = stream.Close()
							self.setStreamingTranscribeStream(nil)
							return false
						}
						return true
					}
					if started && len(preRollFrames) > 0 {
						for _, buffered := range preRollFrames {
							if !sendStreamFrame(buffered) {
								break
							}
						}
					} else {
						_ = sendStreamFrame(frame)
					}
				}
				runActive := self.GetCurrentRunId() != ""
				responseActive := self.GetCurrentResponseId() != ""
				if self.Features.BargeIn && (runActive || responseActive) {
					decision := self.strategy.EvaluateBargeIn(TurnContext{
						VADScore:         score,
						SpeechDurationMs: self.speechDurationMs(time.Now()),
						RunActive:        runActive,
						ResponseActive:   responseActive,
						InterimText:      self.getInterimText(),
					})
					switch decision {
					case TurnDecisionTrigger:
						candidateActive = false
						self.triggerBargeIn()
					case TurnDecisionCandidate:
						if !candidateActive {
							candidateActive = true
							self.sendVoiceEvent("turn.event", turnEventPayload{
								TurnID:   self.GetCurrentTurnId(),
								Event:    "barge_in_candidate",
								VADScore: score,
							})
						}
					default:
						if candidateActive {
							candidateActive = false
							self.sendVoiceEvent("turn.event", turnEventPayload{
								TurnID:   self.GetCurrentTurnId(),
								Event:    "barge_in_suppressed",
								VADScore: score,
							})
						}
					}
				}
			}

			if ended {
				speaking = false
				self.setUserSpeaking(false)
				turnId := self.GetCurrentTurnId()
				nowMs := time.Now().UnixMilli()
				pipelineLog.Infof("voice speech_ended: session=%s turn=%s bytes=%d seq_ref=%d score=%.4f", self.ID, self.GetCurrentTurnId(), len(speechBuf), self.inSeq.Load(), score)
				self.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnId,
					Event:       "speech_ended",
					VADScore:    score,
					AudioSeqRef: self.inSeq.Load(),
				})
				self.notifyObservers(func(observer TurnObserver) {
					observer.OnSpeechEnded(turnId, nowMs)
				})
				if !self.Features.ServerTurn {
					self.setSpeechReady(true)
					continue
				}
				captured := append([]byte(nil), speechBuf...)
				speechBuf = speechBuf[:0]
				candidateActive = false
				if len(captured) < minCommittedTurnBytes {
					pipelineLog.Infof("voice turn ignored (too short): session=%s turn=%s bytes=%d", self.ID, turnId, len(captured))
					self.sendVoiceEvent("turn.event", turnEventPayload{
						TurnID: turnId,
						Event:  "turn_dropped",
						Reason: "dropped_too_short_audio",
					})
					self.notifyObservers(func(observer TurnObserver) {
						observer.OnTurnDropped(turnId, "dropped_too_short_audio", time.Now().UnixMilli())
					})
					continue
				}
				if self.strategy.ShouldCommitTurn(TurnContext{
					SilenceDurationMs: 0,
					InterimText:       self.getInterimText(),
				}) {
					self.commitCapturedTurn(turnId, captured)
					continue
				}
				pendingCommitTurnID = turnId
				pendingCommitAudio = captured
				pendingSilenceMs = 0
			}
		}
	}
}

func (self *Session) streamingTranscribeLoop() {
	stream := self.getStreamingTranscribeStream()
	if stream == nil {
		return
	}
	for {
		select {
		case <-self.doneCh:
			return
		case event, ok := <-stream.Events():
			if !ok {
				self.setStreamingTranscribeStream(nil)
				return
			}
			if event.Err != nil {
				pipelineLog.Warningf("voice streaming stt failed, falling back to batch: %v", event.Err)
				_ = stream.Close()
				self.setStreamingTranscribeStream(nil)
				return
			}
			switch event.Type {
			case "interim":
				self.setInterimText(event.Text)
			case "final":
				turnId := self.GetCurrentTurnId()
				if turnId == "" {
					continue
				}
				self.setInterimText(event.Text)
				// Buffer final streaming text and commit on turn-end path to avoid
				// locking in an early partial final when additional words arrive.
				self.setStreamingFinalText(turnId, event.Text)
			}
		}
	}
}

func (self *Session) commitCapturedTurn(turnId string, captured []byte) {
	if self.getStreamingTranscribeStream() != nil {
		if !self.TryStartTurnTranscription(turnId) {
			pipelineLog.Infof("voice turn transcription skipped (duplicate): session=%s turn=%s", self.ID, turnId)
			return
		}
		go func(tid string, audio []byte) {
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
			if finalText != "" {
				self.handleFinalTranscript(tid, finalText)
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
	go func(tid string, audio []byte) {
		defer self.FinishTurnTranscription(tid)
		self.transcribeAndSend(tid, audio)
	}(turnId, captured)
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
			runId, _ := event["runId"].(string)
			if runId != "" && self.IsRunCanceled(runId) {
				if state == "final" || state == "aborted" || state == "error" {
					self.ClearCanceledRun(runId)
				}
				continue
			}
			if runId != "" && (state == "queued" || state == "delta") {
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
				self.ClearRunTurn(runId)
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
				nowMs := time.Now().UnixMilli()
				pipelineLog.Infof("voice response completed: session=%s response=%s", self.ID, self.GetCurrentResponseId())
				if rid := self.GetCurrentResponseId(); rid != "" {
					turnId := self.GetCurrentResponseTurnID()
					if turnId == "" {
						turnId = self.GetCurrentTurnId()
					}
					self.sendVoiceEvent("response.completed", map[string]interface{}{
						"response_id": rid,
						"turn_id":     turnId,
					})
					self.notifyObservers(func(observer TurnObserver) {
						observer.OnResponseCompleted(turnId, rid, nowMs)
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
			for self.IsUserSpeaking() {
				select {
				case <-self.doneCh:
					return
				case <-time.After(25 * time.Millisecond):
				}
			}
			synth, synthProvider, ok := self.deps.ProviderRegistry().FindSynthesizer()
			if !ok || synth == nil {
				pipelineLog.Warningf("voice synthesis skipped: no synthesizer configured")
				continue
			}
			hadResponse := self.GetCurrentResponseId() != ""
			responseStarted := hadResponse

			ttsCtx, cancel := context.WithCancel(context.Background())
			prev := self.SwapTTSCancel(cancel)
			if prev != nil {
				prev()
			}
			voiceName := "alloy"
			pipelineLog.Infof("voice tts input: session=%s response=%s turn=%s provider=%s model=%s voice=%s text_len=%d text=%q", self.ID, self.GetCurrentResponseId(), self.GetCurrentTurnId(), synthProvider, voiceProviderModelHint("synthesizer", synthProvider), voiceName, len(sentence), sentence)
			chunks, err := synth.SynthesizePCMStream(ttsCtx, sentence, voiceName, self.AudioOut.SampleRateHz)
			if err != nil {
				cancel()
				self.SwapTTSCancel(nil)
				if ttsCtx.Err() != nil {
					continue
				}
				pipelineLog.Warningf("voice synthesis failed: %v", err)
				continue
			}
			if !responseStarted {
				turnId := self.TurnIDForRun(self.GetCurrentRunId())
				if turnId == "" {
					turnId = self.GetCurrentTurnId()
				}
				nowMs := time.Now().UnixMilli()
				self.notifyObservers(func(observer TurnObserver) {
					observer.OnTTSRequested(turnId, nowMs)
				})
			}
			for audio := range chunks {
				if len(audio) == 0 {
					continue
				}
				if !responseStarted {
					// Avoid speaking between two close user utterances while a transcription
					// is still in-flight for a newer turn. For streaming STT, this guard can
					// delay response.started beyond scenario latency budgets, so skip it.
					if self.getStreamingTranscribeStream() == nil {
						start := time.Now()
						for self.HasTranscriptionInFlight() && time.Since(start) < maxResponseStartDelay {
							select {
							case <-self.doneCh:
								return
							case <-time.After(50 * time.Millisecond):
							}
						}
					}
					responseId := self.GetCurrentResponseId()
					if responseId == "" {
						responseId = self.newTurnId()
						self.SetCurrentResponseId(responseId)
					}
					turnId := self.TurnIDForRun(self.GetCurrentRunId())
					if turnId == "" {
						turnId = self.GetCurrentTurnId()
					}
					self.SetCurrentResponseTurnID(turnId)
					nowMs := time.Now().UnixMilli()
					pipelineLog.Infof("voice response started: session=%s response=%s turn=%s", self.ID, responseId, turnId)
					self.sendVoiceEvent("response.started", map[string]interface{}{
						"response_id": responseId,
						"turn_id":     turnId,
					})
					self.notifyObservers(func(observer TurnObserver) {
						observer.OnResponseStarted(turnId, responseId, nowMs)
					})
					responseStarted = true
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
			cancel()
			self.SwapTTSCancel(nil)
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
	transcriber, transcriberProvider, ok := self.deps.ProviderRegistry().FindTranscriber()
	if !ok || transcriber == nil {
		pipelineLog.Warningf("voice transcription skipped: no transcriber configured")
		return
	}
	pipelineLog.Infof("voice transcribe start: session=%s turn=%s bytes=%d provider=%s model=%s", self.ID, turnId, len(captured), transcriberProvider, voiceProviderModelHint("transcriber", transcriberProvider))

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
	self.handleFinalTranscript(turnId, text)
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

func (self *Session) handleFinalTranscript(turnId, rawText string) {
	self.transcribeTextAndSend(turnId, strings.TrimSpace(rawText))
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
	run := self.deps.SendMessage(context.Background(), VoiceSendMessageParams{
		AgentID:            self.AgentID,
		ConversationID:     self.ConversationID,
		Message:            text,
		SystemPromptSuffix: self.effectivePromptSuffix(),
		MaxContextTokens:   voiceMaxContextTokens,
	})
	self.MarkTurnCommitted(turnId)
	self.SetCurrentRunId(run.RunID)
	self.MapRunToTurn(run.RunID, turnId)
	pipelineLog.Infof("voice turn committed: session=%s turn=%s run=%s", self.ID, turnId, run.RunID)
	self.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnId,
		Event:  "turn_committed",
	})
	self.notifyObservers(func(observer TurnObserver) {
		observer.OnTurnCommitted(turnId, time.Now().UnixMilli())
	})
}

func (self *Session) effectivePromptSuffix() string {
	if strings.TrimSpace(self.PromptSuffix) != "" {
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
		self.setLastBargeInAt(time.Now())
		runId := self.GetCurrentRunId()
		self.MarkRunCanceled(runId)
		if prev := self.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		self.drainTTSQueue()
		self.drainAudioOutQueue()
		self.trySendFlushFrame()
		if runId != "" && self.deps != nil {
			self.deps.AbortRun(runId)
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
	self.interimText = ""
	self.interimBestText = ""
	self.streamingFinalTurnID = ""
	self.streamingFinalText = ""
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
