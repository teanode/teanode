package onboarding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/prompts"
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
func InitializeUser(gateway gw.Gateway, userId string) error {
	userId = strings.TrimSpace(userId)
	if userId == "" {
		return fmt.Errorf("userId is required")
	}

	lock := userInitLock(userId)
	lock.Lock()
	defer lock.Unlock()

	if err := configs.EnsureUserDirectories(userId); err != nil {
		return fmt.Errorf("ensuring user directories: %w", err)
	}

	agentId, err := gateway.EnsureDefaultAgent(userId)
	if err != nil {
		return fmt.Errorf("ensure default agent for user: %w", err)
	}

	store := gateway.ConversationStore(userId, agentId)
	if store == nil {
		return fmt.Errorf("conversation store unavailable")
	}

	onboardingExists, err := onboardingExists(userId)
	if err != nil {
		return err
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

func onboardingExists(userId string) (bool, error) {
	userWorkspaceDirectory := configs.UserWorkspaceDirectory(userId)
	onboardingPath := filepath.Join(userWorkspaceDirectory, "ONBOARDING.md")
	var err error
	_, err = os.Stat(onboardingPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading ONBOARDING.md: %w", err)
	}
	return true, nil
}

func storeHasAnyConversation(store *conversations.Store) (bool, error) {
	conversationList, err := store.List()
	if err != nil {
		return false, fmt.Errorf("listing conversations: %w", err)
	}
	return len(conversationList) > 0, nil
}
