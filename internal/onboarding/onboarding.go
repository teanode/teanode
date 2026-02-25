package onboarding

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

// CreateUser creates a user with seed workspace files, a default conversation,
// and an onboarding seed message, all within the caller-provided transaction.
func CreateUser(ctx context.Context, transaction store.Transaction, user *models.User) (*models.User, *models.Conversation, error) {
	if user == nil {
		return nil, nil, fmt.Errorf("user is required")
	}

	// Resolve default agent ID: prefer "main", otherwise earliest CreatedAt.
	agents, err := transaction.ListAgents(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("listing agents: %w", err)
	}
	defaultAgentId := ""
	if len(agents) > 0 {
		for _, agent := range agents {
			if agent.ID == "main" {
				defaultAgentId = "main"
				break
			}
		}
		if defaultAgentId == "" {
			earliest := agents[0]
			for _, agent := range agents[1:] {
				if agent.CreatedAt != nil && earliest.CreatedAt != nil && agent.CreatedAt.Before(*earliest.CreatedAt) {
					earliest = agent
				}
			}
			defaultAgentId = earliest.ID
		}
	}
	if defaultAgentId != "" {
		user.DefaultAgentID = ptrto.Value(defaultAgentId)
	}

	// Build seed workspace files.
	seedWorkspaceFiles := []models.WorkspaceFile{
		{Path: ptrto.Value("USER.md"), Content: byteSlicePtr([]byte(prompts.DefaultUserMarkdown()))},
		{Path: ptrto.Value("ONBOARDING.md"), Content: byteSlicePtr([]byte(prompts.DefaultOnboardingMarkdown()))},
		{Path: ptrto.Value("MEMORY.md"), Content: byteSlicePtr([]byte(prompts.DefaultMemoryMarkdown()))},
	}

	// Create the user with seed workspace files.
	createdUser, createUserError := transaction.CreateUser(ctx, user, seedWorkspaceFiles, nil)
	if createUserError != nil {
		return nil, nil, fmt.Errorf("creating user: %w", createUserError)
	}

	// Create default conversation.
	conversationId := security.NewULID()
	conversation := &models.Conversation{
		ID:         conversationId,
		UserID:     ptrto.Value(createdUser.ID),
		AgentID:    ptrto.Value(defaultAgentId),
		Default:   	ptrto.Value(true),
	}
	createdConversation, err := transaction.CreateConversation(ctx, conversation, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("creating default conversation: %w", err)
	}

	// Create seed onboarding message.
	role := models.Role("assistant")
	content, _ := json.Marshal(prompts.OnboardingSeedMessage)
	message := &models.ConversationMessage{
		ConversationID: ptrto.Value(createdConversation.ID),
		Role:           &role,
		Content:        content,
	}
	if _, err := transaction.CreateConversationMessage(ctx, message, nil); err != nil {
		return nil, nil, fmt.Errorf("creating seed message: %w", err)
	}

	return createdUser, createdConversation, nil
}

func byteSlicePtr(value []byte) *[]byte {
	return &value
}
