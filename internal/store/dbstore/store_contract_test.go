package dbstore_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/dbstore"
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
	settings := dbstore.Settings{
		Host:     envOrDefault("TEANODE_TEST_POSTGRES_HOST", "127.0.0.1"),
		Port:     port,
		User:     envOrDefault("TEANODE_TEST_POSTGRES_USER", "teanode"),
		Password: envOrDefault("TEANODE_TEST_POSTGRES_PASSWORD", "teanode"),
		Database: envOrDefault("TEANODE_TEST_POSTGRES_DATABASE", "teanode"),
		SSLMode:  envOrDefault("TEANODE_TEST_POSTGRES_SSLMODE", "disable"),
	}
	openedStore, openError := dbstore.Open(settings)
	if openError != nil {
		t.Fatalf("Open error: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
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

	transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		createdUser, createError := transaction.CreateUser(context.Background(), &models.User{
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

		foundByUsername, findByUsernameError := transaction.GetUserByUsername(context.Background(), username, nil)
		if findByUsernameError != nil || foundByUsername == nil || foundByUsername.ID != userId {
			t.Fatalf("GetUserByUsername returned unexpected result, want user with ID %q", userId)
		}

		foundByTelegram, findByTelegramError := transaction.GetUserByTelegramChatID(context.Background(), telegramChatId, nil)
		if findByTelegramError != nil || foundByTelegram == nil || foundByTelegram.ID != userId {
			t.Fatalf("GetUserByTelegramChatID returned unexpected result, want user with ID %q", userId)
		}

		foundByDiscord, findByDiscordError := transaction.GetUserByDiscordUserID(context.Background(), discordUserId, nil)
		if findByDiscordError != nil || foundByDiscord == nil || foundByDiscord.ID != userId {
			t.Fatalf("GetUserByDiscordUserID returned unexpected result, want user with ID %q", userId)
		}

		workspaceFile, getFileError := transaction.GetWorkspaceFileByPath(context.Background(), models.ScopeUser, userId, workspacePath, nil)
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

	transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, createError := transaction.CreateUser(context.Background(), &models.User{
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

	verifyError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		_, getError := transaction.GetUser(context.Background(), userId, nil)
		if !errors.Is(getError, store.ErrNotFound) {
			t.Fatalf("GetUser error = %v, want %v", getError, store.ErrNotFound)
		}
		return nil
	})
	if verifyError != nil {
		t.Fatalf("verification transaction error: %v", verifyError)
	}
}

func TestDatabaseStoreJobRunOperations(t *testing.T) {
	openedStore := openDatabaseStore(t)

	userId := security.NewULID()
	username := "job-run-" + security.NewULID()

	transactionError := openedStore.Transaction(context.Background(), func(ctx context.Context, transaction store.Transaction) error {
		if _, createUserError := transaction.CreateUser(ctx, &models.User{
			ID:       userId,
			Username: &username,
		}, nil, nil); createUserError != nil {
			return createUserError
		}

		createdJob, createJobError := transaction.CreateJob(ctx, &models.Job{
			UserID:  ptrto.Value(userId),
			Name:    ptrto.Value("Webhook Job"),
			Prompt:  ptrto.Value("Run report"),
			Enabled: ptrto.Value(true),
		}, nil)
		if createJobError != nil {
			return createJobError
		}

		createdJobRun, createJobRunError := transaction.CreateJobRun(ctx, &models.JobRun{
			JobID:     ptrto.Value(createdJob.ID),
			UserID:    ptrto.Value(userId),
			Trigger:   ptrto.Value(models.JobTriggerKindWebhook),
			Status:    ptrto.Value(models.JobRunStatusRunning),
			StartedAt: ptrto.TimeNowInLocal(),
		}, nil)
		if createJobRunError != nil {
			return createJobRunError
		}

		if _, modifyJobRunError := transaction.ModifyJobRun(ctx, createdJobRun.ID, func(jobRun *models.JobRun) error {
			jobRun.Status = ptrto.Value(models.JobRunStatusSuccess)
			jobRun.RunID = ptrto.Value("run-1")
			return nil
		}, nil); modifyJobRunError != nil {
			return modifyJobRunError
		}

		listedJobRuns, listJobRunsError := transaction.ListJobRuns(ctx, createdJob.ID, nil)
		if listJobRunsError != nil {
			return listJobRunsError
		}
		if len(listedJobRuns) != 1 || listedJobRuns[0].GetRunID() != "run-1" {
			t.Fatalf("unexpected job runs result")
		}
		return nil
	})
	if transactionError != nil {
		t.Fatalf("transaction error: %v", transactionError)
	}
}
