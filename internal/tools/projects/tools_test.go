package projects_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fsstore"
	"github.com/teanode/teanode/internal/tools"
	"github.com/teanode/teanode/internal/tools/projects"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func setupProjectsToolStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(context.Background()); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

func TestProjectsToolCreateListRenameDelete(t *testing.T) {
	openedStore := setupProjectsToolStore(t)
	ctx := store.ContextWithStore(context.Background(), openedStore)
	ctx = models.ContextWithUserSessionToken(ctx, &models.User{ID: "test-user", Admin: ptrto.Value(true)}, nil, nil)

	registry := tools.NewEmptyToolRegistry()
	registry.Register(projects.NewProjectsTool())
	projectsTool := registry.Get("projects")
	if projectsTool == nil {
		t.Fatal("projects tool not registered")
	}

	createResult, createError := projectsTool.Execute(ctx, `{"action":"create","name":"Beta","description":"desc"}`)
	if createError != nil {
		t.Fatalf("create failed: %v", createError)
	}
	var created struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
	}
	if unmarshalError := json.Unmarshal([]byte(createResult), &created); unmarshalError != nil {
		t.Fatalf("unmarshal create result: %v", unmarshalError)
	}
	if created.Project.ID == "" {
		t.Fatal("project ID should not be empty")
	}
	if created.Project.Name != "Beta" {
		t.Fatalf("project name = %q, want Beta", created.Project.Name)
	}

	if _, renameError := projectsTool.Execute(ctx, `{"action":"rename","projectId":"`+created.Project.ID+`","name":"Beta Renamed"}`); renameError != nil {
		t.Fatalf("rename failed: %v", renameError)
	}

	listResult, listError := projectsTool.Execute(ctx, `{"action":"list"}`)
	if listError != nil {
		t.Fatalf("list failed: %v", listError)
	}
	var listed struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if unmarshalError := json.Unmarshal([]byte(listResult), &listed); unmarshalError != nil {
		t.Fatalf("unmarshal list result: %v", unmarshalError)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].Name != "Beta Renamed" {
		t.Fatalf("unexpected projects list: %+v", listed.Projects)
	}

	if _, deleteError := projectsTool.Execute(ctx, `{"action":"delete","projectId":"`+created.Project.ID+`"}`); deleteError != nil {
		t.Fatalf("delete failed: %v", deleteError)
	}
}
