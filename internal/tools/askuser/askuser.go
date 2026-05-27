// Package askuser exposes a tool for requesting user clarification.
package askuser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teanode/teanode/internal/integrations/questions"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/pubsub"
	"github.com/teanode/teanode/internal/runners"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/util/security"
)

func init() {
	tools.RegisterBuiltinTool(func() []tools.Tool {
		return []tools.Tool{&askUserQuestionTool{}}
	})
}

type askUserQuestionTool struct{}

func (self *askUserQuestionTool) Definition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        "ask_user_question",
			Description: "Present a question with choices to the user and wait for their answer. Only works on the web UI channel.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{
						"type":        "string",
						"description": "The question to present to the user.",
					},
					"choices": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"minItems":    2,
						"description": "List of choices the user can pick from.",
					},
					"allowOther": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, show an extra \"Other\" option that lets the user type a freeform answer.",
					},
					"otherLabel": map[string]interface{}{
						"type":        "string",
						"description": "Label for the Other option (default \"Other\").",
					},
					"otherPlaceholder": map[string]interface{}{
						"type":        "string",
						"description": "Placeholder text for the Other text input.",
					},
				},
				"required": []string{"question", "choices"},
			},
		},
	}
}

func (self *askUserQuestionTool) PolicyGroups() []tools.PolicyGroup {
	return []tools.PolicyGroup{
		{Group: models.ToolPolicyGroupAll, Default: models.ToolPolicyAnyone},
	}
}

func (self *askUserQuestionTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	// Channel gate: only supported on webui.
	origin := runners.OriginFromContext(ctx)
	if origin != runners.OriginWeb {
		channel := string(origin)
		if channel == "" {
			channel = "automated"
		}
		result, _ := json.Marshal(map[string]string{
			"error": fmt.Sprintf("ask_user_question is not supported on the %s channel", channel),
		})
		return string(result), nil
	}

	// Parse arguments.
	var arguments struct {
		Question         string   `json:"question"`
		Choices          []string `json:"choices"`
		AllowOther       bool     `json:"allowOther"`
		OtherLabel       string   `json:"otherLabel"`
		OtherPlaceholder string   `json:"otherPlaceholder"`
	}
	if err := json.Unmarshal([]byte(rawArguments), &arguments); err != nil {
		return "", fmt.Errorf("askuser: parsing arguments: %w", err)
	}
	// When allowOther is enabled, strip any choice that duplicates the Other
	// button label so the UI doesn't show "Other" twice.
	if arguments.AllowOther {
		label := arguments.OtherLabel
		if label == "" {
			label = "Other"
		}
		filtered := arguments.Choices[:0]
		for _, choice := range arguments.Choices {
			if !strings.EqualFold(choice, label) {
				filtered = append(filtered, choice)
			}
		}
		arguments.Choices = filtered
	}

	if arguments.Question == "" || len(arguments.Choices) < 2 {
		return "", fmt.Errorf("askuser: question and at least 2 choices are required")
	}

	// Get broker and runner metadata from context.
	broker := questions.QuestionBrokerFromContext(ctx)
	if broker == nil {
		return "", fmt.Errorf("askuser: question broker not available")
	}
	runner := runners.RunnerFromContext(ctx)
	if runner == nil {
		return "", fmt.Errorf("askuser: runner context not available")
	}
	user := models.UserFromContext(ctx)
	if user == nil {
		return "", fmt.Errorf("askuser: authentication required")
	}

	// Register pending question (in-memory only).
	pending := &questions.PendingQuestion{
		ID:               security.NewULID(),
		ConversationID:   runner.ConversationID,
		AgentID:          runner.AgentID,
		UserID:           user.ID,
		RunID:            runner.ID,
		Question:         arguments.Question,
		Choices:          arguments.Choices,
		AllowOther:       arguments.AllowOther,
		OtherLabel:       arguments.OtherLabel,
		OtherPlaceholder: arguments.OtherPlaceholder,
	}
	pending.SetAnswerChan(questions.MakeAnswerChan())
	broker.Register(pending)

	// Broadcast question event via pubsub.
	ps := pubsub.PubSubFromContext(ctx)
	if ps != nil {
		event := map[string]interface{}{
			"action":         "asked",
			"conversationId": pending.ConversationID,
			"agentId":        pending.AgentID,
			"userId":         pending.UserID,
			"runId":          pending.RunID,
			"questionId":     pending.ID,
			"question":       pending.Question,
			"choices":        pending.Choices,
		}
		if pending.AllowOther {
			event["allowOther"] = true
			if pending.OtherLabel != "" {
				event["otherLabel"] = pending.OtherLabel
			}
			if pending.OtherPlaceholder != "" {
				event["otherPlaceholder"] = pending.OtherPlaceholder
			}
		}
		ps.Broadcast(pubsub.EventTypeConversationQuestions, event)
	}

	// Block until answer or cancellation.
	select {
	case payload, ok := <-pending.AnswerChan():
		if !ok {
			return "", fmt.Errorf("askuser: question cancelled")
		}
		result := map[string]string{"answer": payload.Answer}
		if payload.Other != "" {
			result["other"] = payload.Other
		}
		encoded, _ := json.Marshal(result)
		return string(encoded), nil
	case <-ctx.Done():
		broker.Cancel(pending.ID)
		return "", ctx.Err()
	}
}
