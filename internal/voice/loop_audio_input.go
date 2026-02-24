package voice

import "time"

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
				if balanced && self.Features.BargeIn && self.BargeInIsArmed() {
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
				// Snapshot per-frame state once to avoid repeated lock acquisitions.
				currentTurnId := self.GetCurrentTurnId()
				runActive := self.RunIsActive()
				responseActive := self.ResponseIsActive()

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
								TurnID:   currentTurnId,
								Event:    "barge_in_candidate",
								VADScore: score,
							})
						}
					default:
						if candidateActive {
							candidateActive = false
							self.sendVoiceEvent("turn.event", turnEventPayload{
								TurnID:   currentTurnId,
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
