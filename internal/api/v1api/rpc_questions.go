package v1api

import (
	"encoding/json"
	"strings"

	"github.com/teanode/teanode/internal/integrations/questions"
	"github.com/teanode/teanode/internal/pubsub"
)

// --- questions.list ---

func (self *webSocketConnection) handleQuestionsList(frame requestFrame) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.ConversationID == "" {
		self.sendError(frame.ID, 400, "conversationId is required")
		return
	}

	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	broker := self.api.coordinator.QuestionBroker()
	pending := broker.PendingForConversation(parameters.ConversationID)

	// Filter to only questions belonging to this user.
	var result []*questions.PendingQuestion
	for _, q := range pending {
		if q.UserID == self.userId() {
			result = append(result, q)
		}
	}
	if result == nil {
		result = make([]*questions.PendingQuestion, 0)
	}

	self.sendResponse(frame.ID, map[string]interface{}{"questions": result})
}

// --- questions.answer ---
//
// Accepts: { answers: [{ questionId, answer, other? }, ...] }

type answerEntry struct {
	QuestionID string `json:"questionId"`
	Answer     string `json:"answer"`
	Other      string `json:"other,omitempty"`
}

func (self *webSocketConnection) handleQuestionsAnswer(frame requestFrame) {
	var parameters struct {
		Answers []answerEntry `json:"answers"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}

	answers := parameters.Answers
	if len(answers) == 0 {
		self.sendError(frame.ID, 400, "answers array is required and must not be empty")
		return
	}

	// Validate each entry before touching the broker.
	payloads := make(map[string]questions.AnswerPayload, len(answers))
	for _, entry := range answers {
		if entry.QuestionID == "" || entry.Answer == "" {
			self.sendError(frame.ID, 400, "each answer must have questionId and answer")
			return
		}
		if entry.Other != "" && strings.TrimSpace(entry.Other) == "" {
			self.sendError(frame.ID, 400, "other text must not be blank for question "+entry.QuestionID)
			return
		}
		if _, dup := payloads[entry.QuestionID]; dup {
			self.sendError(frame.ID, 400, "duplicate questionId: "+entry.QuestionID)
			return
		}
		payloads[entry.QuestionID] = questions.AnswerPayload{
			Answer: entry.Answer,
			Other:  entry.Other,
		}
	}

	broker := self.api.coordinator.QuestionBroker()

	// Atomic batch: validates all, then delivers all — no partial state.
	if err := broker.AnswerBatch(payloads, self.userId()); err != nil {
		self.sendError(frame.ID, 400, err.Error())
		return
	}

	// Broadcast "answered" events for each question so other tabs dismiss them.
	for _, entry := range answers {
		event := map[string]interface{}{
			"action":     "answered",
			"userId":     self.userId(),
			"questionId": entry.QuestionID,
			"answer":     entry.Answer,
		}
		if entry.Other != "" {
			event["other"] = entry.Other
		}
		self.api.pubsub.Broadcast(pubsub.EventTypeConversationQuestions, event)
	}

	self.sendResponse(frame.ID, map[string]interface{}{"ok": true})
}
