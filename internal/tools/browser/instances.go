package browser

import (
	"fmt"
	"sync"
)

// instanceStore maps user-assigned names to browser connection IDs.
// This enables named browser instances so the LLM can refer to tabs by
// meaningful names (e.g. "login-page", "dashboard") rather than opaque IDs.
type instanceStore struct {
	// names maps userId → (name → connectionId).
	names map[string]map[string]string
	mutex sync.Mutex
}

var globalInstanceStore = &instanceStore{
	names: make(map[string]map[string]string),
}

// assign maps a name to a connectionId for the given user.
func (self *instanceStore) assign(userId string, name string, connectionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.names[userId] == nil {
		self.names[userId] = make(map[string]string)
	}
	self.names[userId][name] = connectionId
}

// resolve returns the connectionId for a named instance.
func (self *instanceStore) resolve(userId string, name string) (string, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	userNames := self.names[userId]
	if userNames == nil {
		return "", fmt.Errorf("no named browser instance %q", name)
	}
	connectionId, ok := userNames[name]
	if !ok {
		return "", fmt.Errorf("no named browser instance %q", name)
	}
	return connectionId, nil
}

// remove deletes a named instance mapping.
func (self *instanceStore) remove(userId string, name string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.names[userId] != nil {
		delete(self.names[userId], name)
	}
}

// listForUser returns all named instances for a user.
func (self *instanceStore) listForUser(userId string) map[string]string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	result := make(map[string]string)
	for name, connectionId := range self.names[userId] {
		result[name] = connectionId
	}
	return result
}
