// Package pubsub provides a simple in-process event broadcasting system.
package pubsub

import (
	"sync"

	"github.com/teanode/teanode/internal/util/deferutil"
)

// EventType identifies the kind of broadcast event.
type EventType string

const (
	// EventTypeConversation is emitted for each stage of an agent run: user_message, queued,
	// delta (streaming text), tool_call, tool_result, final, error, and aborted.
	EventTypeConversation EventType = "conversation"

	// EventTypeConversations signals that the conversation list has changed (created, deleted, or summarized).
	EventTypeConversations EventType = "conversations"

	// EventTypeDefaultAgent is emitted when the system-wide default agent changes.
	EventTypeDefaultAgent EventType = "defaultAgent"

	// EventTypeDefaultConversation is emitted when the default conversation for an agent changes.
	EventTypeDefaultConversation EventType = "defaultConversation"

	// EventTypeJobs signals that the scheduled jobs list has changed.
	EventTypeJobs EventType = "jobs"

	// EventTypeConversationTodos signals that a conversation's todo list has changed.
	EventTypeConversationTodos EventType = "conversation_todos"

	// EventTypeConversationQuestions signals that a question has been asked or answered.
	EventTypeConversationQuestions EventType = "conversation_questions"

	// EventTypeTabToolCall signals a pending tab tool call for the extension to execute.
	EventTypeTabToolCall EventType = "tab_tool_call"

	// EventTypeTabAttachment signals a tab attach/detach event.
	EventTypeTabAttachment EventType = "tab_attachment"
)

// Subscriber receives broadcast events.
type Subscriber interface {
	OnEvent(eventType EventType, payload interface{})
}

// PubSub manages a set of subscribers and broadcasts events to all of them.
type PubSub struct {
	mutex       sync.RWMutex
	subscribers map[Subscriber]struct{}
}

// New creates a new PubSub instance.
func New() *PubSub {
	return &PubSub{
		subscribers: make(map[Subscriber]struct{}),
	}
}

// Subscribe registers a subscriber to receive broadcast events.
func (self *PubSub) Subscribe(subscriber Subscriber) {
	self.mutex.Lock()
	self.subscribers[subscriber] = struct{}{}
	self.mutex.Unlock()
}

// Unsubscribe removes a subscriber.
func (self *PubSub) Unsubscribe(subscriber Subscriber) {
	self.mutex.Lock()
	delete(self.subscribers, subscriber)
	self.mutex.Unlock()
}

// Broadcast sends an event to all subscribers. The subscriber set is
// snapshotted before invoking callbacks so that OnEvent handlers may
// safely call Subscribe/Unsubscribe without deadlocking.
func (self *PubSub) Broadcast(eventType EventType, payload interface{}) {
	self.mutex.RLock()
	snapshot := make([]Subscriber, 0, len(self.subscribers))
	for subscriber := range self.subscribers {
		snapshot = append(snapshot, subscriber)
	}
	self.mutex.RUnlock()

	for _, subscriber := range snapshot {
		func() {
			defer deferutil.Recover()
			subscriber.OnEvent(eventType, payload)
		}()
	}
}
