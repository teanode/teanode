package onboarding

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/conversations"
	"github.com/teanode/teanode/internal/gw"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/util/security"
)

const SeedVersion = 1

type seedMarkerEnvelope struct {
	Teanode struct {
		OnboardingSeedVersion int `json:"onboardingSeedVersion"`
	} `json:"teanode"`
}

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
// conversation if no seed marker exists yet for the user+agent conversation store.
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

	agentId := strings.TrimSpace(gateway.DefaultAgentID())
	if agentId == "" {
		agentId = configs.DefaultAgentID
	}
	store := gateway.ConversationStore(userId, agentId)
	if store == nil {
		return fmt.Errorf("conversation store unavailable")
	}

	seededConversationId, err := findSeededConversation(store)
	if err != nil {
		return err
	}
	if seededConversationId != "" {
		gateway.SetDefaultConversationIfUnset(userId, agentId, seededConversationId)
		return nil
	}

	targetConversationId := security.NewULID()
	if !gateway.SetDefaultConversationIfUnset(userId, agentId, targetConversationId) {
		targetConversationId = gateway.DefaultConversationID(userId, agentId)
	}
	if strings.TrimSpace(targetConversationId) == "" {
		return fmt.Errorf("default conversation id is empty")
	}

	hasSeed, err := conversationHasSeedMarker(store, targetConversationId)
	if err != nil {
		return err
	}
	if hasSeed {
		return nil
	}

	message := conversations.NewTextMessage("assistant", seedMessageText(), time.Now().UnixMilli())
	marker := seedMarkerEnvelope{}
	marker.Teanode.OnboardingSeedVersion = SeedVersion
	metadata, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("marshalling onboarding marker: %w", err)
	}
	message.Metadata = metadata

	model := ""
	provider := ""
	if configuration := gateway.Config(); configuration != nil {
		model = configuration.AgentModel(agentId)
	}
	if model != "" {
		if runner := gateway.ResolveRunner(agentId); runner != nil {
			_, providerRegistry, _, _, _ := runner.Snapshot()
			if providerRegistry != nil {
				provider, _ = providers.ParseQualifiedModel(model, providerRegistry.DefaultProvider())
			}
		}
	}

	options := []conversations.AppendOption{}
	if model != "" {
		options = append(options, conversations.WithProviderAndModel(provider, model))
	}
	if err := store.Append(targetConversationId, message, options...); err != nil {
		return fmt.Errorf("seeding onboarding conversation: %w", err)
	}
	return nil
}

func seedMessageText() string {
	return strings.TrimSpace(`
Welcome! I can help personalize your TeaNode experience.

To get started, tell me:
1. What name should I call you?
2. Do you prefer brief or detailed responses?
3. What language and timezone should I use?
4. What are your top goals for using this assistant?
`)
}

func findSeededConversation(store *conversations.Store) (string, error) {
	items, err := store.List()
	if err != nil {
		return "", fmt.Errorf("listing conversations: %w", err)
	}
	for _, item := range items {
		hasSeed, err := conversationHasSeedMarker(store, item.ID)
		if err != nil {
			return "", err
		}
		if hasSeed {
			return item.ID, nil
		}
	}
	return "", nil
}

func conversationHasSeedMarker(store *conversations.Store, conversationId string) (bool, error) {
	messages, err := store.Load(conversationId)
	if err != nil {
		return false, fmt.Errorf("loading conversation %s: %w", conversationId, err)
	}
	for _, message := range messages {
		if messageHasSeedMarker(message) {
			return true, nil
		}
	}
	return false, nil
}

func messageHasSeedMarker(message conversations.Message) bool {
	if len(message.Metadata) == 0 {
		return false
	}
	var marker seedMarkerEnvelope
	if err := json.Unmarshal(message.Metadata, &marker); err != nil {
		return false
	}
	return marker.Teanode.OnboardingSeedVersion > 0
}
