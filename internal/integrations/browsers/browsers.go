// Package browsers provides a WebSocket relay for browser extension connections.
package browsers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("browsers")

// ConnectedTarget describes a Chrome tab attached via a backend.
type ConnectedTarget struct {
	SessionID string
	TargetID  string
	URL       string
	Title     string
	Source    string // "extension" or "headless"
}

// Browser is the interface that both Relay (extension-backed) and Headless
// (direct CDP) implementations satisfy. All browser tools operate on this
// interface so the backend is transparent.
type Browser interface {
	SendCDPCommand(ctx context.Context, method string, parameters interface{}, sessionId string) (json.RawMessage, error)
	Targets() []ConnectedTarget
	DefaultTarget() (*ConnectedTarget, error)
	TargetByConnectionID(connectionId string) (*ConnectedTarget, error)
	Connected() bool
}

// UserScopedBrowser is a Browser that can scope targets by authenticated user.
type UserScopedBrowser interface {
	TargetsForUser(userId string) []ConnectedTarget
	DefaultTargetForUser(userId string) (*ConnectedTarget, error)
	TargetByConnectionIDForUser(userId, connectionId string) (*ConnectedTarget, error)
}

// TargetOwnerAssigner can label newly-created targets as belonging to a user.
type TargetOwnerAssigner interface {
	AssignTargetToUser(userId, targetId string)
}

// CompositeBrowser merges multiple Browser backends (e.g. headless + relay)
// into a single Browser. Targets from all backends are combined, and commands
// are routed to whichever backend owns the target session.
type CompositeBrowser struct {
	backends []Browser
}

// NewCompositeBrowser creates a composite from one or more backends.
func NewCompositeBrowser(backends ...Browser) *CompositeBrowser {
	return &CompositeBrowser{backends: backends}
}

func (self *CompositeBrowser) Connected() bool {
	for _, backend := range self.backends {
		if backend.Connected() {
			return true
		}
	}
	return false
}

func (self *CompositeBrowser) Targets() []ConnectedTarget {
	var allTargets []ConnectedTarget
	for _, backend := range self.backends {
		allTargets = append(allTargets, backend.Targets()...)
	}
	return allTargets
}

func (self *CompositeBrowser) DefaultTarget() (*ConnectedTarget, error) {
	for _, backend := range self.backends {
		target, err := backend.DefaultTarget()
		if err == nil {
			return target, nil
		}
	}
	return nil, errors.New("no attached browser tab")
}

func (self *CompositeBrowser) TargetByConnectionID(connectionId string) (*ConnectedTarget, error) {
	for _, backend := range self.backends {
		target, err := backend.TargetByConnectionID(connectionId)
		if err == nil {
			return target, nil
		}
	}
	return nil, fmt.Errorf("browser connection %q not found", connectionId)
}

func (self *CompositeBrowser) SendCDPCommand(ctx context.Context, method string, parameters interface{}, sessionId string) (json.RawMessage, error) {
	// Route to the backend that owns this session.
	for _, backend := range self.backends {
		if _, err := backend.TargetByConnectionID(sessionId); err == nil {
			return backend.SendCDPCommand(ctx, method, parameters, sessionId)
		}
	}
	return nil, fmt.Errorf("no backend found for session %q", sessionId)
}

func (self *CompositeBrowser) TargetsForUser(userId string) []ConnectedTarget {
	var allTargets []ConnectedTarget
	for _, backend := range self.backends {
		scoped, ok := backend.(UserScopedBrowser)
		if !ok {
			continue
		}
		allTargets = append(allTargets, scoped.TargetsForUser(userId)...)
	}
	return allTargets
}

func (self *CompositeBrowser) DefaultTargetForUser(userId string) (*ConnectedTarget, error) {
	for _, backend := range self.backends {
		scoped, ok := backend.(UserScopedBrowser)
		if !ok {
			continue
		}
		target, err := scoped.DefaultTargetForUser(userId)
		if err == nil {
			return target, nil
		}
	}
	return nil, errors.New("no attached browser tab")
}

func (self *CompositeBrowser) TargetByConnectionIDForUser(userId, connectionId string) (*ConnectedTarget, error) {
	for _, backend := range self.backends {
		scoped, ok := backend.(UserScopedBrowser)
		if !ok {
			continue
		}
		target, err := scoped.TargetByConnectionIDForUser(userId, connectionId)
		if err == nil {
			return target, nil
		}
	}
	return nil, fmt.Errorf("browser connection %q not found", connectionId)
}
