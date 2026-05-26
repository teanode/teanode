// Package browser — refstore manages the mapping between stable snapshot refs
// and DOM backend node IDs, enabling reference-based browser interactions.
package browser

import (
	"fmt"
	"sync"
)

// referenceEntry holds the mapping from a snapshot reference to its backend DOM node.
type referenceEntry struct {
	BackendDOMNodeID int    // CDP backendDOMNodeId for DOM.resolveNode
	Role             string // accessibility role (e.g. "button", "link")
	Name             string // accessibility name
}

// referenceStore is a per-session store of reference→DOM node mappings. Each snapshot
// overwrites the previous mapping for that session.
type referenceStore struct {
	// sessions maps sessionId → (reference → referenceEntry).
	sessions map[string]map[int]referenceEntry
	mutex    sync.Mutex
}

var globalReferenceStore = &referenceStore{
	sessions: make(map[string]map[int]referenceEntry),
}

// store replaces the reference mapping for the given session.
func (self *referenceStore) store(sessionId string, refs map[int]referenceEntry) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.sessions[sessionId] = refs
}

// lookup returns the referenceEntry for a given session and reference number.
func (self *referenceStore) lookup(sessionId string, reference int) (referenceEntry, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	refs, ok := self.sessions[sessionId]
	if !ok {
		return referenceEntry{}, fmt.Errorf("browser: no snapshot refs for this session — run a snapshot first")
	}
	entry, ok := refs[reference]
	if !ok {
		return referenceEntry{}, fmt.Errorf("browser: reference %d not found in last snapshot — run a new snapshot to refresh refs", reference)
	}
	return entry, nil
}

// clear removes the reference mapping for a session (e.g. on navigation).
func (self *referenceStore) clear(sessionId string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.sessions, sessionId)
}

// interactiveRoles is the set of accessibility roles that receive a reference
// in the snapshot output. These are elements the LLM is likely to interact with.
var interactiveRoles = map[string]bool{
	"button":           true,
	"link":             true,
	"textbox":          true,
	"searchbox":        true,
	"combobox":         true,
	"listbox":          true,
	"option":           true,
	"checkbox":         true,
	"radio":            true,
	"switch":           true,
	"slider":           true,
	"spinbutton":       true,
	"tab":              true,
	"menuitem":         true,
	"menuitemcheckbox": true,
	"menuitemradio":    true,
	"treeitem":         true,
	"row":              true,
	"gridcell":         true,
	"cell":             true,
	"columnheader":     true,
	"rowheader":        true,
}

// isInteractiveRole returns true if the role should receive a stable reference.
func isInteractiveRole(role string) bool {
	return interactiveRoles[role]
}
