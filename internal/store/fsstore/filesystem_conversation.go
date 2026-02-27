package fsstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

func (self *fileSystemTransaction) ListConversations(ctx context.Context, listOptions store.ConversationListOptions, options *store.Option) ([]*models.Conversation, error) {
	return self.listConversations(listOptions, options)
}

func (self *fileSystemTransaction) CreateConversation(ctx context.Context, conversation *models.Conversation, options *store.Option) (*models.Conversation, error) {
	return self.createConversation(conversation, options)
}

func (self *fileSystemTransaction) GetConversation(ctx context.Context, conversationId string, options *store.Option) (*models.Conversation, error) {
	return self.getConversation(conversationId, options)
}

func (self *fileSystemTransaction) FindDefaultConversation(ctx context.Context, userId string, agentId string, options *store.Option) (*models.Conversation, error) {
	return self.findDefaultConversation(ctx, userId, agentId, options)
}

func (self *fileSystemTransaction) SetDefaultConversation(ctx context.Context, conversationId string, options *store.Option) error {
	return self.setDefaultConversation(ctx, conversationId, options)
}

func (self *fileSystemTransaction) ModifyConversation(ctx context.Context, conversationId string, modifier func(*models.Conversation) error, options *store.Option) (*models.Conversation, error) {
	return self.modifyConversation(ctx, conversationId, modifier, options)
}

func (self *fileSystemTransaction) DeleteConversation(ctx context.Context, conversationId string, options *store.Option) error {
	return self.deleteConversation(ctx, conversationId, options)
}

func (self *fileSystemTransaction) listConversations(listOptions store.ConversationListOptions, options *store.Option) ([]*models.Conversation, error) {
	if listOptions.UserID == nil || listOptions.AgentID == nil {
		return []*models.Conversation{}, nil
	}

	conversationDirectory := self.userAgentConversationsDirectory(*listOptions.UserID, *listOptions.AgentID)
	entries, readError := os.ReadDir(conversationDirectory)
	if os.IsNotExist(readError) {
		return []*models.Conversation{}, nil
	}
	if readError != nil {
		return nil, readError
	}

	conversationsList := make([]*models.Conversation, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		conversationId := strings.TrimSuffix(entry.Name(), ".jsonl")
		conversationPath := self.conversationFilePath(*listOptions.UserID, *listOptions.AgentID, conversationId)
		header, headerError := self.loadConversationHeaderByPath(conversationPath)
		if headerError != nil {
			continue
		}
		fileInfo, infoError := entry.Info()
		if infoError != nil {
			continue
		}
		createdAt := fileInfo.ModTime()
		modifiedAt := fileInfo.ModTime()
		if header.Timestamp != "" {
			if parsedTimestamp, parseError := time.Parse(time.RFC3339, header.Timestamp); parseError == nil {
				createdAt = parsedTimestamp
			}
		}
		conversation := models.Conversation{
			ID:         conversationId,
			UserID:     ptrto.TrimmedString(*listOptions.UserID),
			AgentID:    ptrto.TrimmedString(*listOptions.AgentID),
			Title:      ptrto.TrimmedString(header.Title),
			Summary:    ptrto.TrimmedString(header.Summary),
			CreatedAt:  &createdAt,
			ModifiedAt: &modifiedAt,
		}
		if header.SummarizedAt > 0 {
			summarizedAt := time.UnixMilli(header.SummarizedAt)
			conversation.SummarizedAt = &summarizedAt
		}
		conversationCopy := conversation
		conversationsList = append(conversationsList, &conversationCopy)
	}

	sort.Slice(conversationsList, func(leftIndex int, rightIndex int) bool {
		leftConversation := conversationsList[leftIndex]
		rightConversation := conversationsList[rightIndex]
		if leftConversation.ModifiedAt == nil || rightConversation.ModifiedAt == nil {
			return leftConversation.ID < rightConversation.ID
		}
		return leftConversation.ModifiedAt.After(*rightConversation.ModifiedAt)
	})

	return applyOffsetLimit(conversationsList, options), nil
}

func (self *fileSystemTransaction) createConversation(conversation *models.Conversation, options *store.Option) (*models.Conversation, error) {
	if conversation == nil || conversation.UserID == nil || conversation.AgentID == nil {
		return nil, fmt.Errorf("conversation userId and agentId are required")
	}

	conversationId := conversation.ID
	if conversationId == "" {
		conversationId = security.NewULID()
	}

	if createError := self.createConversationFile(*conversation.UserID, *conversation.AgentID, conversationId); createError != nil {
		return nil, createError
	}

	result := *conversation
	result.ID = conversationId
	now := ptrto.TimeNowInLocal()
	result.CreatedAt = now
	result.ModifiedAt = now
	return &result, nil
}

func (self *fileSystemTransaction) getConversation(conversationId string, options *store.Option) (*models.Conversation, error) {
	// Glob for the conversation file across all user/agent directories instead
	// of iterating every user and agent directory pair.
	pattern := filepath.Join(self.usersDirectory(), "*", "conversations", "*", conversationId+".jsonl")
	matches, globError := filepath.Glob(pattern)
	if globError != nil {
		return nil, store.ErrNotFound
	}
	for _, conversationPath := range matches {
		// Extract userId and agentId from the matched path:
		// .../users/{userId}/conversations/{agentId}/{conversationId}.jsonl
		agentId := filepath.Base(filepath.Dir(conversationPath))
		userId := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(conversationPath))))

		header, headerError := self.loadConversationHeaderByPath(conversationPath)
		if headerError != nil {
			continue
		}
		fileInfo, statError := os.Stat(conversationPath)
		if statError != nil {
			continue
		}
		createdAt := fileInfo.ModTime()
		if header.Timestamp != "" {
			if parsedTimestamp, parseError := time.Parse(time.RFC3339, header.Timestamp); parseError == nil {
				createdAt = parsedTimestamp
			}
		}
		modifiedAt := fileInfo.ModTime()
		conversation := &models.Conversation{
			ID:         conversationId,
			UserID:     ptrto.TrimmedString(userId),
			AgentID:    ptrto.TrimmedString(agentId),
			Title:      ptrto.TrimmedString(header.Title),
			Summary:    ptrto.TrimmedString(header.Summary),
			CreatedAt:  &createdAt,
			ModifiedAt: &modifiedAt,
		}
		if header.SummarizedAt > 0 {
			summarizedAt := time.UnixMilli(header.SummarizedAt)
			conversation.SummarizedAt = &summarizedAt
		}
		return conversation, nil
	}
	return nil, store.ErrNotFound
}

func (self *fileSystemTransaction) findDefaultConversation(ctx context.Context, userId string, agentId string, options *store.Option) (*models.Conversation, error) {
	stateData, err := os.ReadFile(self.stateFilename())
	if err != nil {
		return nil, store.ErrNotFound
	}
	var state struct {
		Users map[string]struct {
			DefaultConversationIDs map[string]string `yaml:"defaultConversationIds"`
		} `yaml:"users"`
	}
	if unmarshalError := yaml.Unmarshal(stateData, &state); unmarshalError != nil {
		return nil, unmarshalError
	}
	if state.Users[userId].DefaultConversationIDs == nil {
		return nil, store.ErrNotFound
	}
	conversationId := state.Users[userId].DefaultConversationIDs[agentId]
	if conversationId == "" {
		return nil, store.ErrNotFound
	}
	return self.GetConversation(ctx, conversationId, options)
}

func (self *fileSystemTransaction) setDefaultConversation(ctx context.Context, conversationId string, options *store.Option) error {
	conversation, getError := self.GetConversation(ctx, conversationId, options)
	if getError != nil {
		return getError
	}
	userId := conversation.GetUserID()
	agentId := conversation.GetAgentID()
	if userId == "" || agentId == "" {
		return store.ErrInvalidOptions
	}

	stateFilePath := self.stateFilename()
	stateData, _ := os.ReadFile(stateFilePath)

	var state map[string]interface{}
	if len(stateData) > 0 {
		if unmarshalError := yaml.Unmarshal(stateData, &state); unmarshalError != nil {
			return unmarshalError
		}
	}
	if state == nil {
		state = map[string]interface{}{}
	}

	usersRaw, _ := state["users"].(map[string]interface{})
	if usersRaw == nil {
		usersRaw = map[string]interface{}{}
		state["users"] = usersRaw
	}

	userRaw, _ := usersRaw[userId].(map[string]interface{})
	if userRaw == nil {
		userRaw = map[string]interface{}{}
		usersRaw[userId] = userRaw
	}

	defaultConversationIds, _ := userRaw["defaultConversationIds"].(map[string]interface{})
	if defaultConversationIds == nil {
		defaultConversationIds = map[string]interface{}{}
		userRaw["defaultConversationIds"] = defaultConversationIds
	}

	defaultConversationIds[agentId] = conversationId

	outputData, marshalError := yaml.Marshal(state)
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFile(stateFilePath, outputData)
}

func (self *fileSystemTransaction) modifyConversation(ctx context.Context, conversationId string, modifier func(*models.Conversation) error, options *store.Option) (*models.Conversation, error) {
	conversation, err := self.GetConversation(ctx, conversationId, options)
	if err != nil {
		return nil, err
	}
	if modifierError := modifier(conversation); modifierError != nil {
		return nil, modifierError
	}
	if conversation.UserID == nil || conversation.AgentID == nil {
		return nil, store.ErrInvalidOptions
	}
	if conversation.Title != nil || conversation.Summary != nil || conversation.SummarizedAt != nil {
		updateError := self.updateConversationHeader(*conversation.UserID, *conversation.AgentID, conversationId, func(header *conversationFileHeader) {
			header.Title = conversation.GetTitle()
			header.Summary = conversation.GetSummary()
			if conversation.SummarizedAt != nil {
				header.SummarizedAt = conversation.SummarizedAt.UnixMilli()
			}
		})
		if updateError != nil {
			return nil, updateError
		}
	}
	conversation.ModifiedAt = ptrto.TimeNowInLocal()
	return conversation, nil
}

func (self *fileSystemTransaction) deleteConversation(ctx context.Context, conversationId string, options *store.Option) error {
	conversation, err := self.GetConversation(ctx, conversationId, options)
	if err != nil {
		return err
	}
	if conversation.UserID == nil || conversation.AgentID == nil {
		return store.ErrInvalidOptions
	}
	conversationPath := self.conversationFilePath(*conversation.UserID, *conversation.AgentID, conversationId)
	if _, statError := os.Stat(conversationPath); os.IsNotExist(statError) {
		return nil
	}
	return trash.Move(conversationPath, self.trashDirectory())
}
