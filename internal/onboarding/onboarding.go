package onboarding

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/prompts"
	"github.com/teanode/teanode/internal/store"
)

var userInitLocks sync.Map // map[userId]*sync.Mutex

func userInitLock(userId string) *sync.Mutex {
	if lock, ok := userInitLocks.Load(userId); ok {
		return lock.(*sync.Mutex)
	}
	lock := &sync.Mutex{}
	actual, _ := userInitLocks.LoadOrStore(userId, lock)
	return actual.(*sync.Mutex)
}

// InitializeUser ensures user directories/workspace and seeds one onboarding
// assistant message when ONBOARDING.md exists and the user has no user-authored messages yet.
func InitializeUser(ctx context.Context, gateway gw.Gateway, userId string) error {
	userId = strings.TrimSpace(userId)
	if userId == "" {
		return fmt.Errorf("userId is required")
	}

	lock := userInitLock(userId)
	lock.Lock()
	defer lock.Unlock()

	if err := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		return seedUserWorkspace(transaction, userId)
	}); err != nil {
		return fmt.Errorf("seeding user workspace: %w", err)
	}

	agentId, err := gateway.EnsureDefaultAgent(userId)
	if err != nil {
		return fmt.Errorf("ensure default agent for user: %w", err)
	}

	store := gateway.ConversationStore(userId, agentId)

	onboardingExists, onboardingErr := workspaceFileExists(ctx, models.ScopeUser, userId, "ONBOARDING.md")
	if onboardingErr != nil {
		return onboardingErr
	}
	if !onboardingExists {
		return nil
	}

	hasConversation, err := storeHasAnyConversation(store)
	if err != nil {
		return err
	}
	if hasConversation {
		return nil
	}

	conversationId := gateway.NewDefaultConversation(userId, agentId, "")
	message := conversations.NewTextMessage("assistant", prompts.OnboardingSeedMessage, time.Now().UnixMilli())
	if err := store.Append(conversationId, message); err != nil {
		return fmt.Errorf("seeding onboarding conversation: %w", err)
	}
	return nil
}

func seedUserWorkspace(transaction store.Transaction, userId string) error {
	if _, getError := transaction.GetWorkspaceFileByPath(models.ScopeUser, userId, "USER.md", nil); getError == nil {
		return nil
	}
	filesToSeed := []struct {
		path    string
		content string
	}{
		{path: "USER.md", content: prompts.DefaultUserMarkdown()},
		{path: "ONBOARDING.md", content: prompts.DefaultOnboardingMarkdown()},
		{path: "MEMORY.md", content: prompts.DefaultMemoryMarkdown()},
	}
	scope := models.ScopeUser
	scopeID := userId
	for _, fileToSeed := range filesToSeed {
		if _, err := transaction.GetWorkspaceFileByPath(scope, scopeID, fileToSeed.path, nil); err == nil {
			continue
		}
		content := []byte(fileToSeed.content)
		relativePath := fileToSeed.path
		if _, err := transaction.CreateWorkspaceFile(&models.WorkspaceFile{
			ID:      "",
			Scope:   &scope,
			ScopeID: &scopeID,
			Path:    &relativePath,
			Content: &content,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

func workspaceFileExists(ctx context.Context, scope models.Scope, scopeID string, relativePath string) (bool, error) {
	exists := false
	err := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		if _, getError := transaction.GetWorkspaceFileByPath(scope, scopeID, relativePath, nil); getError == nil {
			exists = true
		}
		return nil
	})
	return exists, err
}

func storeHasAnyConversation(store *conversations.Store) (bool, error) {
	conversationList, err := store.List()
	if err != nil {
		return false, fmt.Errorf("listing conversations: %w", err)
	}
	return len(conversationList) > 0, nil
}
