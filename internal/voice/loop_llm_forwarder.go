package voice

import "strings"

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
