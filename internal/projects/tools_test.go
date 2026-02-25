package projects_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fs"
)

func setupProjectsToolStore(t *testing.T) store.Store {
	t.Helper()
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: t.TempDir()})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() { _ = openedStore.Close() })
	return openedStore
}

func TestProjectsToolCreateAndWrite(t *testing.T) {
	openedStore := setupProjectsToolStore(t)
	ctx := store.ContextWithStore(context.Background(), openedStore)

	registry := agents.NewToolRegistry()
	registry.Register(projects.NewProjectsTool())
	registry.Register(projects.NewProjectWorkspaceTool())
	projectsTool := registry.Get("projects")
	workspaceTool := registry.Get("project_workspace")
	if projectsTool == nil {
		t.Fatal("projects tool not registered")
	}
	if workspaceTool == nil {
		t.Fatal("project_workspace tool not registered")
	}

	result, createError := projectsTool.Execute(ctx, `{"action":"create","name":"Alpha","description":"Track milestones and shared project decisions","purpose":"Track milestones"}`)
	if createError != nil {
		t.Fatalf("create failed: %v", createError)
	}
	var created struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
	}
	if unmarshalError := json.Unmarshal([]byte(result), &created); unmarshalError != nil {
		t.Fatalf("unmarshal create result: %v", unmarshalError)
	}
	if created.Project.ID == "" {
		t.Fatal("project ID should not be empty")
	}
	if created.Project.Name != "Alpha" {
		t.Fatalf("project name = %q, want Alpha", created.Project.Name)
	}

	time.Sleep(2 * time.Millisecond)
	if _, writeError := workspaceTool.Execute(ctx, `{"action":"write","projectId":"`+created.Project.ID+`","path":"notes.md","content":"hello"}`); writeError != nil {
		t.Fatalf("write failed: %v", writeError)
	}
	readResult, readError := workspaceTool.Execute(ctx, `{"action":"read","projectId":"`+created.Project.ID+`","path":"notes.md"}`)
	if readError != nil {
		t.Fatalf("read failed: %v", readError)
	}
	if !strings.Contains(readResult, "hello") {
		t.Fatalf("read result = %q, want to contain hello", readResult)
	}
}

func TestProjectsToolListRenameDelete(t *testing.T) {
	openedStore := setupProjectsToolStore(t)
	ctx := store.ContextWithStore(context.Background(), openedStore)

	registry := agents.NewToolRegistry()
	registry.Register(projects.NewProjectsTool())
	registry.Register(projects.NewProjectWorkspaceTool())
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
			ID string `json:"id"`
		} `json:"project"`
	}
	if unmarshalError := json.Unmarshal([]byte(createResult), &created); unmarshalError != nil {
		t.Fatalf("unmarshal create result: %v", unmarshalError)
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
