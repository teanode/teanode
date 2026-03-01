package askuser

import (
	"fmt"
	"sync"
)

// PendingQuestion represents a question waiting for a user answer.
// The answerChan is unexported and exists only in memory.
type PendingQuestion struct {
	ID             string   `json:"id"`
	ConversationID string   `json:"conversationId"`
	AgentID        string   `json:"agentId"`
	UserID         string   `json:"userId"`
	RunID          string   `json:"runId"`
	ToolCallID     string   `json:"toolCallId"`
	Question       string   `json:"question"`
	Choices        []string `json:"choices"`
	answerChan     chan string
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
func (b *QuestionBroker) Answer(questionId, answer string) error {
	b.mu.Lock()
	q, ok := b.pending[questionId]
	if ok {
		delete(b.pending, questionId)
	}
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("question not found or already answered: %s", questionId)
	}
	q.answerChan <- answer
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

// MakeAnswerChan creates a buffered channel for a PendingQuestion.
// This is a helper used by the tool's Execute method.
func MakeAnswerChan() chan string {
	return make(chan string, 1)
}
