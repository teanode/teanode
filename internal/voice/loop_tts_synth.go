package voice

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/teanode/teanode/internal/providers"
)

func (self *Session) ttsSynthLoop() {
	for {
		select {
		case <-self.doneCh:
			return
		case sentence := <-self.ttsInCh:
			if sentence == "" {
				nowMs := time.Now().UnixMilli()
				pipelineLog.Infof("voice response completed: session=%s response=%s", self.ID, self.GetCurrentResponseID())
				if rid := self.GetCurrentResponseID(); rid != "" {
					turnId := self.GetCurrentResponseTurnID()
					if turnId == "" {
						turnId = self.GetCurrentTurnID()
					}
					self.sendVoiceEvent("response.completed", map[string]interface{}{
						"responseId": rid,
						"turnId":     turnId,
					})
					self.notifyObservers(func(observer TurnObserver) {
						observer.OnResponseCompleted(turnId, rid, nowMs)
					})
				}
				self.ClearCurrentResponse()
				continue
			}
			if self.dispatcher == nil {
				pipelineLog.Warningf("voice synthesis skipped: missing dispatcher")
				continue
			}
			if self.dispatcher.ProviderRegistry() == nil {
				pipelineLog.Warningf("voice synthesis skipped: provider registry unavailable")
				continue
			}
			// Wait without polling until the user stops speaking.
			// getUserSpeakingCh returns an open channel while speaking and nil
			// when silent; select on a nil channel blocks forever, so we only
			// enter the select when there is actually something to wait on.
			if speakingChannel := self.getUserSpeakingCh(); speakingChannel != nil {
				select {
				case <-self.doneCh:
					return
				case <-speakingChannel:
					// channel closed by setUserSpeaking(false) - user stopped speaking
				}
			}
			synth, synthesizerProvider, ok := self.dispatcher.ProviderRegistry().FindSynthesizer()
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
			pipelineLog.Infof("voice tts input: session=%s response=%s turn=%s provider=%s model=%s voice=%s text_len=%d text=%q", self.ID, self.GetCurrentResponseID(), self.GetCurrentTurnID(), synthesizerProvider, voiceProviderModelHint("synthesizer", synthesizerProvider), voiceName, len(sentence), sentence)
			chunks, err := synthesizeToChunks(ttsCtx, synth, sentence, voiceName, self.AudioOut.SampleRateHz)
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
				turnId := self.TurnIDForRun(self.GetCurrentRunID())
				if turnId == "" {
					turnId = self.GetCurrentTurnID()
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
					responseId := self.GetCurrentResponseID()
					if responseId == "" {
						responseId = self.newTurnId()
						self.SetCurrentResponseID(responseId)
					}
					turnId := self.TurnIDForRun(self.GetCurrentRunID())
					if turnId == "" {
						turnId = self.GetCurrentTurnID()
					}
					self.SetCurrentResponseTurnID(turnId)
					nowMs := time.Now().UnixMilli()
					pipelineLog.Infof("voice response started: session=%s response=%s turn=%s", self.ID, responseId, turnId)
					self.sendVoiceEvent("response.started", map[string]interface{}{
						"responseId": responseId,
						"turnId":     turnId,
					})
					self.notifyObservers(func(observer TurnObserver) {
						observer.OnResponseStarted(turnId, responseId, nowMs)
					})
					responseStarted = true
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
			cancel()
			self.SwapTTSCancel(nil)
		}
	}
}

// synthesizeToChunks converts a SynthesizeProvider call into a channel of PCM chunks.
// It tries StreamingSynthesizeProvider first, falling back to batch Synthesize.
func synthesizeToChunks(ctx context.Context, synth providers.SynthesizeProvider, text, voice string, sampleRateHz int) (<-chan []byte, error) {
	// Try streaming synthesis first.
	if streamer, ok := synth.(providers.StreamingSynthesizeProvider); ok {
		chunks, err := streamer.SynthesizeStream(ctx, providers.SynthesizeStreamRequest{
			Text:         text,
			Voice:        voice,
			SampleRateHz: sampleRateHz,
		})
		if err == nil {
			out := make(chan []byte, 32)
			go func() {
				defer close(out)
				for chunk := range chunks {
					if chunk.Err != nil {
						return
					}
					if len(chunk.Audio) > 0 {
						select {
						case out <- chunk.Audio:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
			return out, nil
		}
	}
	// Fall back to batch synthesis.
	response, err := synth.Synthesize(ctx, providers.SynthesizeRequest{
		Text:   text,
		Voice:  voice,
		Format: "wav",
		Speed:  1.0,
	})
	if err != nil {
		return nil, err
	}
	defer response.Audio.Close()
	wavData, _ := io.ReadAll(response.Audio)
	pcm, err := wavToPCM16LE(wavData)
	if err != nil {
		return nil, fmt.Errorf("batch tts wav decode: %w", err)
	}
	out := make(chan []byte, 1)
	if len(pcm) > 0 {
		out <- pcm
	}
	close(out)
	return out, nil
}
