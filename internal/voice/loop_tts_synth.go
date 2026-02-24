package voice

import (
	"context"
	"time"
)

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
			// Wait without polling until the user stops speaking.
			// getUserSpeakingCh returns an open channel while speaking and nil
			// when silent; select on a nil channel blocks forever, so we only
			// enter the select when there is actually something to wait on.
			if ch := self.getUserSpeakingCh(); ch != nil {
				select {
				case <-self.doneCh:
					return
				case <-ch:
					// channel closed by setUserSpeaking(false) - user stopped speaking
				}
			}
			synth, synthProvider, ok := self.deps.ProviderRegistry().FindSynthesizer()
			if !ok || synth == nil {
				pipelineLog.Warningf("voice synthesis skipped: no synthesizer configured")
				continue
			}
			hadResponse := self.ResponseIsActive()
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
