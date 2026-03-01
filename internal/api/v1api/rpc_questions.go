package v1api

import (
	"encoding/json"
	"strings"

	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/tools/askuser"

	"log/slog"
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
	var result []*askuser.PendingQuestion
	for _, q := range pending {
		if q.UserID == self.userId() {
			result = append(result, q)
		}
	}
	if result == nil {
		result = make([]*askuser.PendingQuestion, 0)
	}

	self.sendResponse(frame.ID, map[string]interface{}{"questions": result})
}

// --- questions.answer ---

func (self *webSocketConnection) handleQuestionsAnswer(frame requestFrame) {
	var parameters struct {
		QuestionID string `json:"questionId"`
		Answer     string `json:"answer"`
		Other      string `json:"other"` // freeform text when "Other" is selected
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if parameters.QuestionID == "" || parameters.Answer == "" {
		self.sendError(frame.ID, 400, "questionId and answer are required")
		return
	}

	broker := self.api.coordinator.QuestionBroker()

	// Verify the caller owns this question.
	if err := broker.VerifyOwnership(parameters.QuestionID, self.userId()); err != nil {
		self.sendError(frame.ID, 403, err.Error())
		return
	}

	// Validate: if Other text is expected (answer matches the other label), text must be non-empty.
	if parameters.Other != "" && strings.TrimSpace(parameters.Other) == "" {
		self.sendError(frame.ID, 400, "other text must not be blank")
		return
	}

	payload := askuser.AnswerPayload{
		Answer: parameters.Answer,
		Other:  parameters.Other,
	}
	if err := broker.Answer(parameters.QuestionID, payload); err != nil {
		self.sendError(frame.ID, 404, err.Error())
		return
	}

	// Broadcast "answered" event so other tabs dismiss the question.
	event := map[string]interface{}{
		"action":     "answered",
		"userId":     self.userId(),
		"questionId": parameters.QuestionID,
		"answer":     parameters.Answer,
	}
	if parameters.Other != "" {
		event["other"] = parameters.Other
	}
	self.api.pubsub.Broadcast(pubsub.EventTypeConversationQuestions, event)

	self.sendResponse(frame.ID, map[string]interface{}{"ok": true})
}

// --- questions.answer_batch ---

type batchAnswerEntry struct {
	QuestionID string `json:"questionId"`
	Answer     string `json:"answer"`
	Other      string `json:"other,omitempty"`
}

func (self *webSocketConnection) handleQuestionsAnswerBatch(frame requestFrame) {
	var parameters struct {
		Answers []batchAnswerEntry `json:"answers"`
	}
	if frame.Params != nil {
		json.Unmarshal(frame.Params, &parameters)
	}
	if len(parameters.Answers) == 0 {
		self.sendError(frame.ID, 400, "answers array is required and must not be empty")
		return
	}

	// Validate each entry before touching the broker.
	payloads := make(map[string]askuser.AnswerPayload, len(parameters.Answers))
	for _, entry := range parameters.Answers {
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
		payloads[entry.QuestionID] = askuser.AnswerPayload{
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
	for _, entry := range parameters.Answers {
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

	slog.Info("questions.answer_batch", "count", len(parameters.Answers), "user", self.userId())
	self.sendResponse(frame.ID, map[string]interface{}{"ok": true})
}
