package fs_test

import (
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fs"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func openFileSystemStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("Open error: %v", openError)
	}
	t.Cleanup(func() {
		_ = openedStore.Close()
	})
	return openedStore
}

func TestFileSystemStoreUserOperationWithWorkspaceSeed(t *testing.T) {
	openedStore := openFileSystemStore(t)

	userId := "user-1"
	username := "alice"
	telegramChatId := int64(101)
	discordUserId := "discord-alice"
	workspacePath := "AGENTS.md"
	workspaceContent := []byte("system instructions")

	transactionError := openedStore.Transaction(func(transaction store.Transaction) error {
		createdUser, createError := transaction.CreateUser(&models.User{
			ID:             userId,
			Username:       &username,
			TelegramChatID: &telegramChatId,
			DiscordUserID:  &discordUserId,
		}, []models.WorkspaceFile{
			{
				Scope:   ptrto.Value(models.ScopeUser),
				ScopeID: ptrto.Value(userId),
				Path:    ptrto.Value(workspacePath),
				Content: &workspaceContent,
			},
		}, nil)
		if createError != nil {
			return createError
		}
		if createdUser.ID != userId {
			t.Fatalf("created user ID = %q, want %q", createdUser.ID, userId)
		}

		foundId, _, foundByUsername := transaction.GetUserByUsername(username, nil)
		if !foundByUsername || foundId != userId {
			t.Fatalf("GetUserByUsername returned (%q, %v), want (%q, true)", foundId, foundByUsername, userId)
		}

		foundId, _, foundByTelegram := transaction.GetUserByTelegramChatID(telegramChatId, nil)
		if !foundByTelegram || foundId != userId {
			t.Fatalf("GetUserByTelegramChatID returned (%q, %v), want (%q, true)", foundId, foundByTelegram, userId)
		}

		foundId, _, foundByDiscord := transaction.GetUserByDiscordUserID(discordUserId, nil)
		if !foundByDiscord || foundId != userId {
			t.Fatalf("GetUserByDiscordUserID returned (%q, %v), want (%q, true)", foundId, foundByDiscord, userId)
		}

		workspaceFile, getFileError := transaction.GetWorkspaceFileByPath(models.ScopeUser, userId, workspacePath, nil)
		if getFileError != nil {
			return getFileError
		}
		if workspaceFile.Content == nil || string(*workspaceFile.Content) != string(workspaceContent) {
			t.Fatalf("workspace content mismatch")
		}
		return nil
	})
	if transactionError != nil {
		t.Fatalf("Transaction error: %v", transactionError)
	}
}

func TestFileSystemStoreConversationAndMessageOperations(t *testing.T) {
	openedStore := openFileSystemStore(t)

	userId := "user-1"
	agentId := "agent-main"
	role := models.RoleUser
	content := []byte(`"hello"`)

	transactionError := openedStore.Transaction(func(transaction store.Transaction) error {
		conversation, createConversationError := transaction.CreateConversation(&models.Conversation{
			UserID:  &userId,
			AgentID: &agentId,
		}, nil)
		if createConversationError != nil {
			return createConversationError
		}

		createdMessage, createMessageError := transaction.CreateConversationMessage(&models.ConversationMessage{
			ConversationID: ptrto.Value(conversation.ID),
			Role:           &role,
			Content:        &content,
		}, nil)
		if createMessageError != nil {
			return createMessageError
		}

		listedMessages, listError := transaction.ListConversationMessages(conversation.ID, nil)
		if listError != nil {
			return listError
		}
		if len(listedMessages) != 1 {
			t.Fatalf("message count = %d, want 1", len(listedMessages))
		}
		if listedMessages[0].ID != createdMessage.ID {
			t.Fatalf("message ID mismatch")
		}
		return nil
	})
	if transactionError != nil {
		t.Fatalf("Transaction error: %v", transactionError)
	}
}

func TestFileSystemStoreSessionJobAndSkillOperations(t *testing.T) {
	openedStore := openFileSystemStore(t)

	userId := "user-1"
	userAgent := "Mozilla"
	remoteAddress := "127.0.0.1"
	enabled := true
	skillName := "sample-skill"
	skillVersion := "1.2.3"
	skillPrompt := "do things"
	skillMetadata := map[string]interface{}{"description": "Sample"}
	skillTools := []map[string]interface{}{
		{
			"name":        "echo",
			"description": "echo tool",
			"type":        "shell",
			"parameters": map[string]interface{}{
				"type": "object",
			},
			"command": []string{"echo", "hello"},
		},
	}

	transactionError := openedStore.Transaction(func(transaction store.Transaction) error {
		createdSession, createSessionError := transaction.CreateSession(&models.Session{
			UserID:        &userId,
			UserAgent:     &userAgent,
			RemoteAddress: &remoteAddress,
			ExpiresAt:     ptrto.Value(time.Now().Add(24 * time.Hour)),
		}, nil)
		if createSessionError != nil {
			return createSessionError
		}
		if createdSession.ID == "" {
			t.Fatalf("session ID is empty")
		}

		createdJob, createJobError := transaction.CreateJob(&models.Job{
			UserID:  &userId,
			Name:    ptrto.Value("Daily"),
			Prompt:  ptrto.Value("Run report"),
			Enabled: &enabled,
		}, nil)
		if createJobError != nil {
			return createJobError
		}
		loadedJob, getJobError := transaction.GetJob(createdJob.ID, nil)
		if getJobError != nil {
			return getJobError
		}
		if loadedJob.Name == nil || *loadedJob.Name != "Daily" {
			t.Fatalf("loaded job name mismatch")
		}

		createdSkill, createSkillError := transaction.CreateSkill(&models.Skill{
			Name:        &skillName,
			Description: ptrto.TrimmedString("Sample"),
			Version:     &skillVersion,
			Prompt:      &skillPrompt,
			Tools:       &skillTools,
			Metadata:    &skillMetadata,
		}, nil)
		if createSkillError != nil {
			return createSkillError
		}
		loadedSkill, getSkillError := transaction.GetSkill(createdSkill.ID, nil)
		if getSkillError != nil {
			return getSkillError
		}
		if loadedSkill.Prompt == nil || *loadedSkill.Prompt != skillPrompt {
			t.Fatalf("loaded skill prompt mismatch")
		}
		if loadedSkill.Tools == nil {
			t.Fatalf("loaded skill tools are nil")
		}
		if len(*loadedSkill.Tools) == 0 {
			t.Fatalf("loaded skill tools are empty")
		}
		return nil
	})
	if transactionError != nil {
		t.Fatalf("Transaction error: %v", transactionError)
	}
}
