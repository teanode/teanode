package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/coordinators"
	"github.com/teanode/teanode/internal/integrations/surfaces"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/util/security"
)

// --- surfaces.list ---
//
// Returns active surfaces and interrupts for a conversation so the client can
// rehydrate generative-UI state after a reconnect.

func (self *webSocketConnection) handleSurfacesList(frame requestFrame) (interface{}, error) {
	var parameters struct {
		ConversationID string `json:"conversationId"`
	}
	if frame.Parameters != nil {
		_ = json.Unmarshal(frame.Parameters, &parameters)
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}
	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		return nil, rpcError(403, err.Error())
	}

	broker := self.api.coordinator.SurfaceBroker()
	surfaceList := broker.SurfacesForConversation(parameters.ConversationID)
	interruptList := broker.InterruptsForConversation(parameters.ConversationID)

	sort.Slice(surfaceList, func(leftIndex, rightIndex int) bool {
		return surfaceList[leftIndex].SurfaceID < surfaceList[rightIndex].SurfaceID
	})
	sort.Slice(interruptList, func(leftIndex, rightIndex int) bool {
		return interruptList[leftIndex].InterruptID < interruptList[rightIndex].InterruptID
	})

	return map[string]interface{}{
		"surfaces":   surfaceList,
		"interrupts": interruptList,
	}, nil
}

// --- surfaces.emit ---
//
// A direct emission path (no LLM required) used to drive and test the
// generative-UI stack end to end. Accepts a surface and/or an interrupt and
// broadcasts it to the conversation.

func (self *webSocketConnection) handleSurfacesEmit(frame requestFrame) (interface{}, error) {
	var parameters struct {
		ConversationID string              `json:"conversationId"`
		AgentID        string              `json:"agentId,omitempty"`
		Surface        *surfaces.Surface   `json:"surface,omitempty"`
		Interrupt      *surfaces.Interrupt `json:"interrupt,omitempty"`
	}
	if frame.Parameters != nil {
		if err := json.Unmarshal(frame.Parameters, &parameters); err != nil {
			return nil, rpcError(400, "invalid parameters: "+err.Error())
		}
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}
	if parameters.Surface == nil && parameters.Interrupt == nil {
		return nil, rpcError(400, "surface or interrupt is required")
	}
	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		return nil, rpcError(403, err.Error())
	}

	agentId := parameters.AgentID
	if agentId == "" {
		agentId = self.defaultAgentId()
	}

	broker := self.api.coordinator.SurfaceBroker()

	event := map[string]interface{}{
		"action":         "emitted",
		"conversationId": parameters.ConversationID,
		"agentId":        agentId,
	}

	if parameters.Surface != nil {
		surface := parameters.Surface
		if surface.SurfaceID == "" {
			surface.SurfaceID = security.NewULID()
		}
		surface.SchemaVersion = surfaces.SchemaVersion
		surface.ConversationID = parameters.ConversationID
		surface.AgentID = agentId
		if err := surface.Validate(); err != nil {
			return nil, rpcError(400, err.Error())
		}
		broker.RegisterSurface(surface)
		event["surface"] = surface
	}

	if parameters.Interrupt != nil {
		interrupt := parameters.Interrupt
		if interrupt.InterruptID == "" {
			interrupt.InterruptID = security.NewULID()
		}
		interrupt.ConversationID = parameters.ConversationID
		interrupt.AgentID = agentId
		if err := interrupt.Validate(); err != nil {
			return nil, rpcError(400, err.Error())
		}
		broker.RegisterInterrupt(interrupt)
		event["interrupt"] = interrupt
	}

	self.api.pubsub.Broadcast(pubsub.EventTypeConversationSurfaces, event)

	return map[string]interface{}{"ok": true}, nil
}

// --- surfaces.action ---
//
// Routes a user action on a surface back through the conversation runtime. For
// the MVP the action is injected as a synthetic, user-visible message that
// resumes the conversation loop, and any interrupt routed through the surface is
// cleared. This routes through the coordinator/session layer without needing a
// general patch engine.

func (self *webSocketConnection) handleSurfacesAction(frame requestFrame) (interface{}, error) {
	var parameters struct {
		ConversationID string                        `json:"conversationId"`
		Action         surfaces.SurfaceActionPayload `json:"action"`
	}
	if frame.Parameters != nil {
		if err := json.Unmarshal(frame.Parameters, &parameters); err != nil {
			return nil, rpcError(400, "invalid parameters: "+err.Error())
		}
	}
	if parameters.ConversationID == "" {
		return nil, rpcError(400, "conversationId is required")
	}
	if parameters.Action.ActionID == "" {
		return nil, rpcError(400, "action.actionId is required")
	}
	if err := self.verifyConversationAccess(parameters.ConversationID); err != nil {
		return nil, rpcError(403, err.Error())
	}

	broker := self.api.coordinator.SurfaceBroker()

	agentId := self.defaultAgentId()
	var surface *surfaces.Surface
	if parameters.Action.SurfaceID != "" {
		surface = broker.LookupSurface(parameters.Action.SurfaceID)
		if surface != nil && surface.AgentID != "" {
			agentId = surface.AgentID
		}
	}

	message := buildSurfaceActionMessage(surface, parameters.Action)

	// Clear interrupts routed through this surface and notify clients.
	if parameters.Action.SurfaceID != "" {
		for _, interrupt := range broker.InterruptsForSurface(parameters.Action.SurfaceID) {
			broker.RemoveInterrupt(interrupt.InterruptID)
			self.api.pubsub.Broadcast(pubsub.EventTypeConversationSurfaces, map[string]interface{}{
				"action":         "removed",
				"conversationId": parameters.ConversationID,
				"interruptId":    interrupt.InterruptID,
			})
		}
	}

	handle, runError := self.api.coordinator.Run(self.ctx, coordinators.RunParameters{
		AgentID:         agentId,
		ConversationID:  parameters.ConversationID,
		Message:         message,
		Origin:          runners.OriginWeb,
		OriginSessionID: self.sessionId(),
	}, nil)
	if runError != nil {
		return nil, rpcError(500, runError.Error())
	}

	return map[string]interface{}{
		"ok":             true,
		"runId":          handle.RunID,
		"conversationId": handle.ConversationID,
	}, nil
}

// buildSurfaceActionMessage turns a surface action into a concise, user-visible
// message the agent can react to.
func buildSurfaceActionMessage(surface *surfaces.Surface, action surfaces.SurfaceActionPayload) string {
	label := action.ActionID
	if surface != nil {
		if buttonLabel := findButtonLabel(surface, action.ActionID); buttonLabel != "" {
			label = buttonLabel
		}
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("[UI action] %s", label))
	if action.Value != "" {
		builder.WriteString(": " + action.Value)
	}
	if len(action.FormData) > 0 {
		keys := make([]string, 0, len(action.FormData))
		for key := range action.FormData {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, key := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%s", key, action.FormData[key]))
		}
		builder.WriteString("\n" + strings.Join(pairs, "\n"))
	}
	return builder.String()
}

// findButtonLabel searches a surface's components for a button with the given
// action id and returns its label, or "".
func findButtonLabel(surface *surfaces.Surface, actionId string) string {
	var search func(components []surfaces.SurfaceComponent) string
	search = func(components []surfaces.SurfaceComponent) string {
		for _, component := range components {
			for _, button := range component.Buttons {
				if button.ActionID == actionId {
					return button.Label
				}
			}
			if component.SubmitActionID == actionId && component.SubmitLabel != "" {
				return component.SubmitLabel
			}
			if found := search(component.Children); found != "" {
				return found
			}
		}
		return ""
	}
	return search(surface.Components)
}
