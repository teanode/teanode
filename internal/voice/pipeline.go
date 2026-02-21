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
	bargeInTriggerMinScore = 0.06
)

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
				turnID := s.newTurnID()
				s.startNewTurn(turnID)
				pipelineLog.Infof("voice speech_started: session=%s turn=%s seq_ref=%d score=%.4f", s.ID, turnID, s.inSeq.Load(), score)
				s.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnID,
					Event:       "speech_started",
					VADScore:    score,
					AudioSeqRef: s.inSeq.Load(),
				})
				if s.Features.BargeIn && score >= bargeInTriggerMinScore && s.GetCurrentResponseID() != "" {
					s.triggerBargeIn()
				}
			}

			if vad.IsSpeaking {
				speechBuf = append(speechBuf, frame...)
			}

			if ended {
				turnID := s.GetCurrentTurnID()
				pipelineLog.Infof("voice speech_ended: session=%s turn=%s bytes=%d seq_ref=%d score=%.4f", s.ID, s.GetCurrentTurnID(), len(speechBuf), s.inSeq.Load(), score)
				s.sendVoiceEvent("turn.event", turnEventPayload{
					TurnID:      turnID,
					Event:       "speech_ended",
					VADScore:    score,
					AudioSeqRef: s.inSeq.Load(),
				})
				captured := append([]byte(nil), speechBuf...)
				speechBuf = speechBuf[:0]
				if len(captured) < minCommittedTurnBytes {
					pipelineLog.Infof("voice turn ignored (too short): session=%s turn=%s bytes=%d", s.ID, turnID, len(captured))
					continue
				}
				if !s.TryStartTurnTranscription(turnID) {
					pipelineLog.Infof("voice turn transcription skipped (duplicate): session=%s turn=%s", s.ID, turnID)
					continue
				}
				go func(tid string, audio []byte) {
					defer s.FinishTurnTranscription(tid)
					s.transcribeAndSend(tid, audio)
				}(turnID, captured)
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
			if state == "queued" || state == "final" || state == "error" || state == "aborted" {
				pipelineLog.Debugf("voice llm event: session=%s turn=%s state=%s text_len=%d run=%s", s.ID, s.GetCurrentTurnID(), state, len(text), s.GetCurrentRunID())
			}
			if state == "delta" {
				newSentences, nextCount := ExtractCompleteSentences(streamText, sentencesEnqueued)
				sentencesEnqueued = nextCount
				if len(newSentences) > 0 {
					pipelineLog.Debugf("voice sentence enqueue: session=%s count=%d total=%d", s.ID, len(newSentences), sentencesEnqueued)
				}
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
				// Response stream is complete; allow next transcript to commit a new run.
				s.ClearCurrentRun()
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
				pipelineLog.Infof("voice response completed: session=%s response=%s", s.ID, s.GetCurrentResponseID())
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
				pipelineLog.Infof("voice response started: session=%s response=%s turn=%s", s.ID, responseID, s.GetCurrentTurnID())
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
			pipelineLog.Debugf("voice tts bytes: session=%s response=%s sentence_len=%d bytes=%d", s.ID, s.GetCurrentResponseID(), len(sentence), len(audio))
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

func (s *Session) transcribeAndSend(turnID string, captured []byte) {
	if len(captured) == 0 || s.deps == nil || s.deps.ProviderRegistry() == nil {
		return
	}
	pipelineLog.Debugf("voice transcribe start: session=%s turn=%s bytes=%d", s.ID, turnID, len(captured))
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
		pipelineLog.Infof("voice transcript ignored (empty): session=%s turn=%s", s.ID, turnID)
		return
	}
	if len([]rune(text)) < minCommittedTextRunes {
		pipelineLog.Infof("voice transcript ignored (too short): session=%s turn=%s text=%q", s.ID, turnID, text)
		return
	}
	if s.GetCurrentRunID() != "" {
		pipelineLog.Infof("voice transcript ignored (run in flight): session=%s turn=%s run=%s", s.ID, turnID, s.GetCurrentRunID())
		return
	}
	if s.IsTurnCommitted(turnID) {
		pipelineLog.Infof("voice transcript ignored (already committed): session=%s turn=%s", s.ID, turnID)
		return
	}
	pipelineLog.Infof("voice transcript.final: session=%s turn=%s text_len=%d", s.ID, turnID, len(text))
	s.sendVoiceEvent("transcript.final", map[string]interface{}{
		"turn_id": turnID,
		"text":    text,
	})
	run := s.deps.SendMessage(context.Background(), VoiceSendMessageParams{
		AgentID:        s.AgentID,
		ConversationID: s.ConversationID,
		Message:        text,
	})
	s.MarkTurnCommitted(turnID)
	s.SetCurrentRunID(run.RunID)
	pipelineLog.Infof("voice turn committed: session=%s turn=%s run=%s", s.ID, turnID, run.RunID)
	s.sendVoiceEvent("turn.event", turnEventPayload{
		TurnID: turnID,
		Event:  "turn_committed",
	})
}

func (s *Session) triggerBargeIn() {
	s.bargeInOnce.Do(func() {
		pipelineLog.Infof("voice barge_in triggered: session=%s run=%s response=%s", s.ID, s.GetCurrentRunID(), s.GetCurrentResponseID())
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
	pipelineLog.Debugf("voice flush frame queued: session=%s", s.ID)
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
	state, _ := eventMap["state"].(string)
	critical := state == "final" || state == "error" || state == "aborted" || state == "queued"
	if !critical {
		select {
		case s.eventCh <- eventMap:
		default:
		}
		return
	}

	select {
	case s.eventCh <- eventMap:
	default:
		// Preserve terminal lifecycle events by making room if queue is saturated by deltas.
		select {
		case <-s.eventCh:
		default:
		}
		select {
		case s.eventCh <- eventMap:
		default:
		}
	}
}
