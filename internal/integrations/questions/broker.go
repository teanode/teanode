package questions

import (
	"fmt"
	"sync"
)

// AnswerPayload carries the user's answer through the broker channel.
type AnswerPayload struct {
	Answer string // The selected choice (or the otherLabel for freeform).
	Other  string // Non-empty when the user chose the "Other" option.
}

// PendingQuestion represents a question waiting for a user answer.
// The answerChan is unexported and exists only in memory.
type PendingQuestion struct {
	ID               string   `json:"id"`
	ConversationID   string   `json:"conversationId"`
	AgentID          string   `json:"agentId"`
	UserID           string   `json:"userId"`
	RunID            string   `json:"runId"`
	ToolCallID       string   `json:"toolCallId"`
	Question         string   `json:"question"`
	Choices          []string `json:"choices"`
	AllowOther       bool     `json:"allowOther,omitempty"`
	OtherLabel       string   `json:"otherLabel,omitempty"`
	OtherPlaceholder string   `json:"otherPlaceholder,omitempty"`
	answerChan       chan AnswerPayload
}

// AnswerChan returns the answer channel.
func (self *PendingQuestion) AnswerChan() chan AnswerPayload {
	return self.answerChan
}

// SetAnswerChan sets the answer channel.
func (self *PendingQuestion) SetAnswerChan(channel chan AnswerPayload) {
	self.answerChan = channel
}

// QuestionBroker is an in-memory registry that routes answers from the
// WebSocket RPC layer to blocked tool Execute() goroutines.
type QuestionBroker struct {
	mutex   sync.Mutex
	pending map[string]*PendingQuestion // questionId -> question
}

// NewQuestionBroker creates a new broker.
func NewQuestionBroker() *QuestionBroker {
	return &QuestionBroker{
		pending: make(map[string]*PendingQuestion),
	}
}

// Register adds a pending question to the broker.
func (self *QuestionBroker) Register(question *PendingQuestion) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.pending[question.ID] = question
}

// Answer delivers an answer to a pending question and removes it from the broker.
// Returns an error if the question is not found (already answered or cancelled).
func (self *QuestionBroker) Answer(questionId string, payload AnswerPayload) error {
	self.mutex.Lock()
	question, ok := self.pending[questionId]
	if ok {
		delete(self.pending, questionId)
	}
	self.mutex.Unlock()
	if !ok {
		return fmt.Errorf("questions: question not found or already answered: %s", questionId)
	}
	question.answerChan <- payload
	return nil
}

// Cancel removes a pending question and closes its channel.
func (self *QuestionBroker) Cancel(questionId string) {
	self.mutex.Lock()
	question, ok := self.pending[questionId]
	if ok {
		delete(self.pending, questionId)
	}
	self.mutex.Unlock()
	if ok {
		close(question.answerChan)
	}
}

// PendingForConversation returns all pending questions for a conversation.
func (self *QuestionBroker) PendingForConversation(conversationId string) []*PendingQuestion {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var result []*PendingQuestion
	for _, question := range self.pending {
		if question.ConversationID == conversationId {
			result = append(result, question)
		}
	}
	return result
}

// VerifyOwnership checks that the caller is the owner of the question.
func (self *QuestionBroker) VerifyOwnership(questionId, callerUserId string) error {
	self.mutex.Lock()
	question, ok := self.pending[questionId]
	self.mutex.Unlock()
	if !ok {
		return fmt.Errorf("questions: question not found: %s", questionId)
	}
	if question.UserID != callerUserId {
		return fmt.Errorf("questions: not authorized to answer this question")
	}
	return nil
}

// AnswerBatch atomically delivers answers to multiple questions.
// It validates all questions first (existence + ownership), and only delivers
// if every question is valid — avoiding partial state.
func (self *QuestionBroker) AnswerBatch(answers map[string]AnswerPayload, callerUserId string) error {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Phase 1: validate all questions exist and belong to the caller.
	questions := make(map[string]*PendingQuestion, len(answers))
	for questionId := range answers {
		question, ok := self.pending[questionId]
		if !ok {
			return fmt.Errorf("questions: question not found or already answered: %s", questionId)
		}
		if question.UserID != callerUserId {
			return fmt.Errorf("questions: not authorized to answer question: %s", questionId)
		}
		questions[questionId] = question
	}

	// Phase 2: all valid — remove from pending and deliver.
	for questionId, question := range questions {
		delete(self.pending, questionId)
		question.answerChan <- answers[questionId]
	}
	return nil
}

// MakeAnswerChan creates a buffered channel for a PendingQuestion.
// This is a helper used by the tool's Execute method.
func MakeAnswerChan() chan AnswerPayload {
	return make(chan AnswerPayload, 1)
}
