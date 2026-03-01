package askuser

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

// QuestionBroker is an in-memory registry that routes answers from the
// WebSocket RPC layer to blocked tool Execute() goroutines.
type QuestionBroker struct {
	mu      sync.Mutex
	pending map[string]*PendingQuestion // questionId -> question
}

// NewQuestionBroker creates a new broker.
func NewQuestionBroker() *QuestionBroker {
	return &QuestionBroker{
		pending: make(map[string]*PendingQuestion),
	}
}

// Register adds a pending question to the broker.
func (b *QuestionBroker) Register(q *PendingQuestion) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[q.ID] = q
}

// Answer delivers an answer to a pending question and removes it from the broker.
// Returns an error if the question is not found (already answered or cancelled).
func (b *QuestionBroker) Answer(questionId string, payload AnswerPayload) error {
	b.mu.Lock()
	q, ok := b.pending[questionId]
	if ok {
		delete(b.pending, questionId)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("question not found or already answered: %s", questionId)
	}
	q.answerChan <- payload
	return nil
}

// Cancel removes a pending question and closes its channel.
func (b *QuestionBroker) Cancel(questionId string) {
	b.mu.Lock()
	q, ok := b.pending[questionId]
	if ok {
		delete(b.pending, questionId)
	}
	b.mu.Unlock()
	if ok {
		close(q.answerChan)
	}
}

// PendingForConversation returns all pending questions for a conversation.
func (b *QuestionBroker) PendingForConversation(conversationId string) []*PendingQuestion {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result []*PendingQuestion
	for _, q := range b.pending {
		if q.ConversationID == conversationId {
			result = append(result, q)
		}
	}
	return result
}

// VerifyOwnership checks that the caller is the owner of the question.
func (b *QuestionBroker) VerifyOwnership(questionId, callerUserId string) error {
	b.mu.Lock()
	q, ok := b.pending[questionId]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("question not found: %s", questionId)
	}
	if q.UserID != callerUserId {
		return fmt.Errorf("not authorized to answer this question")
	}
	return nil
}

// AnswerBatch atomically delivers answers to multiple questions.
// It validates all questions first (existence + ownership), and only delivers
// if every question is valid — avoiding partial state.
func (b *QuestionBroker) AnswerBatch(answers map[string]AnswerPayload, callerUserId string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Phase 1: validate all questions exist and belong to the caller.
	questions := make(map[string]*PendingQuestion, len(answers))
	for qid := range answers {
		q, ok := b.pending[qid]
		if !ok {
			return fmt.Errorf("question not found or already answered: %s", qid)
		}
		if q.UserID != callerUserId {
			return fmt.Errorf("not authorized to answer question: %s", qid)
		}
		questions[qid] = q
	}

	// Phase 2: all valid — remove from pending and deliver.
	for qid, q := range questions {
		delete(b.pending, qid)
		q.answerChan <- answers[qid]
	}
	return nil
}

// MakeAnswerChan creates a buffered channel for a PendingQuestion.
// This is a helper used by the tool's Execute method.
func MakeAnswerChan() chan AnswerPayload {
	return make(chan AnswerPayload, 1)
}
