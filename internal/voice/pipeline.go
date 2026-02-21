package voice

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
)

var pipelineLog = logging.MustGetLogger("voice.pipeline")

func (s *Session) audioInputLoop() {
	vad := &VADState{}
	var speechBuf []byte

	for {
		select {
		case <-s.doneCh:
			return
		case frame := <-s.audioInCh:
			started, ended, score := vad.ProcessFrame(frame)
			if started {
				s.startNewTurn(s.newTurnID())
				s.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      s.GetCurrentTurnID(),
					Event:       "speech_started",
					VADScore:    score,
					AudioSeqRef: s.inSeq.Load(),
				})
				if s.Features.BargeIn && s.GetCurrentResponseID() != "" {
					s.triggerBargeIn()
				}
			}

			if vad.IsSpeaking {
				speechBuf = append(speechBuf, frame...)
			}

			if ended {
				s.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      s.GetCurrentTurnID(),
					Event:       "speech_ended",
					VADScore:    score,
					AudioSeqRef: s.inSeq.Load(),
				})
				captured := append([]byte(nil), speechBuf...)
				speechBuf = speechBuf[:0]
				go s.transcribeAndSend(captured)
			}
		}
	}
}

func (s *Session) llmEventForwarder() {
	if s.deps == nil {
		return
	}
	sub := &conversationEventSubscriber{
		conversationID: s.ConversationID,
		eventCh:        make(chan map[string]interface{}, 128),
	}
	s.deps.Subscribe(sub)
	defer s.deps.Unsubscribe(sub)

	streamText := ""
	sentencesEnqueued := 0

	for {
		select {
		case <-s.doneCh:
			return
		case event := <-sub.eventCh:
			state, _ := event["state"].(string)
			text, _ := event["text"].(string)
			if text != "" {
				streamText += text
			}
			if runID, _ := event["runId"].(string); runID != "" && (state == "queued" || state == "delta") {
				s.SetCurrentRunID(runID)
			}
			if state == "delta" {
				newSentences, nextCount := ExtractCompleteSentences(streamText, sentencesEnqueued)
				sentencesEnqueued = nextCount
				for _, sentence := range newSentences {
					select {
					case s.ttsInCh <- sentence:
					case <-s.doneCh:
						return
					}
				}
			}
			if state == "final" || state == "aborted" || state == "error" {
				remaining := strings.TrimSpace(FlushRemaining(streamText, sentencesEnqueued))
				if remaining != "" {
					select {
					case s.ttsInCh <- remaining:
					case <-s.doneCh:
						return
					}
				}
				select {
				case s.ttsInCh <- "":
				case <-s.doneCh:
					return
				}
				streamText = ""
				sentencesEnqueued = 0
			}
		}
	}
}

func (s *Session) ttsSynthLoop() {
	for {
		select {
		case <-s.doneCh:
			return
		case sentence := <-s.ttsInCh:
			if sentence == "" {
				if rid := s.GetCurrentResponseID(); rid != "" {
					s.sendVoiceEvent("response.completed", map[string]interface{}{
						"response_id": rid,
						"turn_id":     s.GetCurrentTurnID(),
					})
				}
				s.ClearCurrentResponse()
				continue
			}
			if s.deps == nil || s.deps.ProviderRegistry() == nil {
				continue
			}
			synth, _, ok := s.deps.ProviderRegistry().FindSynthesizer()
			if !ok || synth == nil {
				continue
			}
			responseID := s.GetCurrentResponseID()
			if responseID == "" {
				responseID = s.newTurnID()
				s.SetCurrentResponseID(responseID)
				s.sendVoiceEvent("response.started", map[string]interface{}{
					"response_id": responseID,
					"turn_id":     s.GetCurrentTurnID(),
				})
			}

			ttsCtx, cancel := context.WithCancel(context.Background())
			prev := s.SwapTTSCancel(cancel)
			if prev != nil {
				prev()
			}
			audio, err := synth.SynthesizePCM(ttsCtx, sentence, "alloy", s.AudioOut.SampleRateHz)
			s.SwapTTSCancel(nil)
			if err != nil {
				if ttsCtx.Err() != nil {
					continue
				}
				pipelineLog.Warningf("voice synthesis failed: %v", err)
				continue
			}
			payload := EncodeBinaryAudioFrame(BinaryAudioFrame{
				FrameType:   FrameTypeAudioOut,
				Seq:         s.NextOutSeq(),
				CaptureTSMs: time.Now().UnixMilli(),
				DurationMS:  0,
				Data:        audio,
			})
			s.enqueueAudioOut(payload)
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

func (s *Session) transcribeAndSend(captured []byte) {
	if len(captured) == 0 || s.deps == nil || s.deps.ProviderRegistry() == nil {
		return
	}
	transcriber, _, ok := s.deps.ProviderRegistry().FindTranscriber()
	if !ok || transcriber == nil {
		return
	}

	wav := PCMToWAV(captured, s.AudioIn.SampleRateHz, s.AudioIn.Channels)
	result, err := transcriber.Transcribe(context.Background(), VoiceTranscribeRequest{
		Audio:      wav,
		Format:     "wav",
		SampleRate: s.AudioIn.SampleRateHz,
		Channels:   s.AudioIn.Channels,
	})
	if err != nil || result == nil {
		if err != nil {
			pipelineLog.Warningf("voice transcription failed: %v", err)
		}
		return
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return
	}
	s.sendVoiceEvent("transcript.final", map[string]interface{}{
		"turn_id": s.GetCurrentTurnID(),
		"text":    text,
	})
	run := s.deps.SendMessage(context.Background(), VoiceSendMessageParams{
		AgentID:        s.AgentID,
		ConversationID: s.ConversationID,
		Message:        text,
	})
	s.SetCurrentRunID(run.RunID)
	s.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: s.GetCurrentTurnID(),
		Event:  "turn_committed",
	})
}

func (s *Session) triggerBargeIn() {
	s.bargeInOnce.Do(func() {
		if prev := s.SwapTTSCancel(nil); prev != nil {
			prev()
		}
		s.trySendFlushFrame()
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
}

func (s *Session) trySendFlushFrame() {
	payload := EncodeBinaryAudioFrame(BinaryAudioFrame{
		FrameType:   FrameTypeFlush,
		Seq:         s.NextOutSeq(),
		CaptureTSMs: time.Now().UnixMilli(),
		DurationMS:  0,
	})
	s.enqueueAudioOut(payload)
}

type conversationEventSubscriber struct {
	conversationID string
	eventCh        chan map[string]interface{}
}

func (s *conversationEventSubscriber) OnVoiceEvent(eventType string, payload interface{}) {
	if eventType != "conversation" {
		return
	}
	eventMap, ok := payload.(map[string]interface{})
	if !ok {
		return
	}
	conversationID, _ := eventMap["conversationId"].(string)
	if conversationID != s.conversationID {
		return
	}
	select {
	case s.eventCh <- eventMap:
	default:
	}
}
