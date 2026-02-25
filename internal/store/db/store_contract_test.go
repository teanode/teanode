package db_test

import (
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storedb "github.com/teanode/teanode/internal/store/db"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
)

func openDatabaseStore(t *testing.T) store.Store {
	t.Helper()
	if os.Getenv("TEANODE_TEST_POSTGRES") != "1" {
		t.Skip("set TEANODE_TEST_POSTGRES=1 to run postgres store tests")
	}
	port := uint16(5432)
	if portValue := os.Getenv("TEANODE_TEST_POSTGRES_PORT"); portValue != "" {
		parsedPort, parseError := strconv.ParseUint(portValue, 10, 16)
		if parseError != nil {
			t.Fatalf("invalid TEANODE_TEST_POSTGRES_PORT: %v", parseError)
		}
		port = uint16(parsedPort)
	}
	settings := storedb.Settings{
		Host:     envOrDefault("TEANODE_TEST_POSTGRES_HOST", "127.0.0.1"),
		Port:     port,
		User:     envOrDefault("TEANODE_TEST_POSTGRES_USER", "teanode"),
		Password: envOrDefault("TEANODE_TEST_POSTGRES_PASSWORD", "teanode"),
		Database: envOrDefault("TEANODE_TEST_POSTGRES_DATABASE", "teanode"),
		SSLMode:  envOrDefault("TEANODE_TEST_POSTGRES_SSLMODE", "disable"),
	}
	openedStore, openError := storedb.Open(settings)
	if openError != nil {
		t.Fatalf("Open error: %v", openError)
	}
	if migrateError := openedStore.Migrate(); migrateError != nil {
		t.Fatalf("Migrate error: %v", migrateError)
	}
	t.Cleanup(func() {
		_ = openedStore.Close()
	})
	return openedStore
}

func envOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func TestDatabaseStoreUserOperationWithWorkspaceSeed(t *testing.T) {
	openedStore := openDatabaseStore(t)

	userId := security.NewULID()
	username := "user-" + security.NewULID()
	telegramChatId := time.Now().UnixNano()
	discordUserId := "discord-" + security.NewULID()
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

		foundUserId, _, foundByUsername := transaction.GetUserByUsername(username, nil)
		if !foundByUsername || foundUserId != userId {
			t.Fatalf("GetUserByUsername returned (%q, %v), want (%q, true)", foundUserId, foundByUsername, userId)
		}

		foundUserId, _, foundByTelegram := transaction.GetUserByTelegramChatID(telegramChatId, nil)
		if !foundByTelegram || foundUserId != userId {
			t.Fatalf("GetUserByTelegramChatID returned (%q, %v), want (%q, true)", foundUserId, foundByTelegram, userId)
		}

		foundUserId, _, foundByDiscord := transaction.GetUserByDiscordUserID(discordUserId, nil)
		if !foundByDiscord || foundUserId != userId {
			t.Fatalf("GetUserByDiscordUserID returned (%q, %v), want (%q, true)", foundUserId, foundByDiscord, userId)
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

func TestDatabaseStoreTransactionRollback(t *testing.T) {
	openedStore := openDatabaseStore(t)

	userId := security.NewULID()
	username := "rollback-" + security.NewULID()

	transactionError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, createError := transaction.CreateUser(&models.User{
			ID:       userId,
			Username: &username,
		}, nil, nil)
		if createError != nil {
			return createError
		}
		return errors.New("force rollback")
	})
	if transactionError == nil {
		t.Fatalf("expected transaction error")
	}

	verifyError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, getError := transaction.GetUser(userId, nil)
		if !errors.Is(getError, store.ErrNotFound) {
			t.Fatalf("GetUser error = %v, want %v", getError, store.ErrNotFound)
		}
		return nil
	})
	if verifyError != nil {
		t.Fatalf("verification transaction error: %v", verifyError)
	}
}
