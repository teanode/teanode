package projects_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/projects"
	"github.com/teanode/teanode/internal/util/timeutil"
)

func withTempDirForTools(t *testing.T) {
	t.Helper()
	configs.SetDirectory(t.TempDir())
	t.Cleanup(func() { configs.SetDirectory("") })
}

func TestProjectsToolCreateAndWrite(t *testing.T) {
	withTempDirForTools(t)
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

	ctx := context.Background()
	result, err := projectsTool.Execute(ctx, `{"action":"create","name":"Alpha","description":"Track milestones and shared project decisions","purpose":"Track milestones"}`)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	var created struct {
		Project struct {
			ID        string             `json:"id"`
			Name      string             `json:"name"`
			UpdatedAt timeutil.Timestamp `json:"updatedAt"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(result), &created); err != nil {
		t.Fatalf("unmarshal create result: %v", err)
	}
	if created.Project.ID == "" {
		t.Fatal("project ID should not be empty")
	}
	if created.Project.Name != "Alpha" {
		t.Fatalf("project name = %q, want Alpha", created.Project.Name)
	}

	time.Sleep(2 * time.Millisecond)
	_, err = workspaceTool.Execute(ctx, `{"action":"write","projectId":"`+created.Project.ID+`","path":"notes.md","content":"hello"}`)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	listedProjects, err := configs.LoadProjectConfigs()
	if err != nil {
		t.Fatalf("LoadProjectConfigs(): %v", err)
	}
	if len(listedProjects) != 1 {
		t.Fatalf("len(listedProjects) = %d, want 1", len(listedProjects))
	}
	metadata := listedProjects[0]
	if metadata.ID != created.Project.ID {
		t.Fatalf("metadata.ID = %q, want %q", metadata.ID, created.Project.ID)
	}
	if !metadata.UpdatedAt.Time.After(created.Project.UpdatedAt.Time) {
		t.Fatalf("updatedAt = %s, want > %s", metadata.UpdatedAt.String(), created.Project.UpdatedAt.String())
	}
}

func TestProjectsToolListRenameDelete(t *testing.T) {
	withTempDirForTools(t)
	registry := agents.NewToolRegistry()
	registry.Register(projects.NewProjectsTool())
	registry.Register(projects.NewProjectWorkspaceTool())
	projectsTool := registry.Get("projects")
	if projectsTool == nil {
		t.Fatal("projects tool not registered")
	}

	ctx := context.Background()
	result, err := projectsTool.Execute(ctx, `{"action":"create","name":"Beta","description":"desc"}`)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	var created struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(result), &created); err != nil {
		t.Fatalf("unmarshal create result: %v", err)
	}

	_, err = projectsTool.Execute(ctx, `{"action":"rename","projectId":"`+created.Project.ID+`","name":"Beta Renamed"}`)
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	result, err = projectsTool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	var listed struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(result), &listed); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].Name != "Beta Renamed" {
		t.Fatalf("unexpected projects list: %+v", listed.Projects)
	}

	_, err = projectsTool.Execute(ctx, `{"action":"delete","projectId":"`+created.Project.ID+`"}`)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestProjectsAndWorkspaceToolActions(t *testing.T) {
	withTempDirForTools(t)
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

	ctx := context.Background()
	result, err := projectsTool.Execute(ctx, `{"action":"create","name":"Aliases","description":"desc"}`)
	if err != nil {
		t.Fatalf("create alias failed: %v", err)
	}
	var created struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(result), &created); err != nil {
		t.Fatalf("unmarshal create result: %v", err)
	}
	if created.Project.ID == "" {
		t.Fatal("project ID should not be empty")
	}

	if _, err := projectsTool.Execute(ctx, `{"action":"info","projectId":"`+created.Project.ID+`"}`); err != nil {
		t.Fatalf("info failed: %v", err)
	}
	if _, err := workspaceTool.Execute(ctx, `{"action":"list","projectId":"`+created.Project.ID+`"}`); err != nil {
		t.Fatalf("workspace list failed: %v", err)
	}
	if _, err := projectsTool.Execute(ctx, `{"action":"delete","projectId":"`+created.Project.ID+`"}`); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
}

func TestProjectsToolCreateRequiresDescription(t *testing.T) {
	withTempDirForTools(t)
	registry := agents.NewToolRegistry()
	registry.Register(projects.NewProjectsTool())
	registry.Register(projects.NewProjectWorkspaceTool())
	projectsTool := registry.Get("projects")
	if projectsTool == nil {
		t.Fatal("projects tool not registered")
	}

	_, err := projectsTool.Execute(context.Background(), `{"action":"create","name":"NoDesc"}`)
	if err == nil {
		t.Fatal("expected error when description is missing for create")
	}
	if err.Error() != "description is required for create" {
		t.Fatalf("unexpected error: %v", err)
	}
}
