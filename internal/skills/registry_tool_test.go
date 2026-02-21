package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
)

func TestRegisterTools(t *testing.T) {
	registry := agents.NewToolRegistry()
	RegisterTools(registry, nil, nil)
	if registry.Get("skills") == nil {
		t.Fatal("skills tool was not registered")
	}
}

func TestDefinition(t *testing.T) {
	tool := &skillsTool{}
	definition := tool.Definition()
	if definition.Function.Name != "skills" {
		t.Fatalf("name = %q, want skills", definition.Function.Name)
	}
}

func TestExecuteUnknownAction(t *testing.T) {
	tool := &skillsTool{}
	_, err := tool.Execute(context.Background(), `{"action":"nope"}`)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestExecuteListRegistryNilConfig(t *testing.T) {
	tool := &skillsTool{}
	out, err := tool.Execute(context.Background(), `{"action":"list_registry"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if payload["action"] != "list_registry" {
		t.Fatalf("action = %v, want list_registry", payload["action"])
	}
}

func TestExecuteInstallRequiresName(t *testing.T) {
	tool := &skillsTool{registries: []configs.SkillsRegistry{{ID: "source"}}}
	_, err := tool.Execute(context.Background(), `{"action":"install"}`)
	if err == nil {
		t.Fatal("expected error when name missing")
	}
}

func TestExecuteUninstallRequiresName(t *testing.T) {
	tool := &skillsTool{}
	_, err := tool.Execute(context.Background(), `{"action":"uninstall"}`)
	if err == nil {
		t.Fatal("expected error when name missing")
	}
}

func TestExecuteUninstallCallsSkillsChangedCallback(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	installDirectory := filepath.Join(directory, "skills", ".installed", "demo", "1.0.0")
	if err := os.MkdirAll(installDirectory, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDirectory, "skill.md"), []byte("x"), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	var callbackCalls int32
	tool := &skillsTool{
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}
	if _, err := tool.Execute(context.Background(), `{"action":"uninstall","name":"demo"}`); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if got := atomic.LoadInt32(&callbackCalls); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestExecuteUpdateNoChangesDoesNotCallSkillsChangedCallback(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	var callbackCalls int32
	tool := &skillsTool{
		registries: []configs.SkillsRegistry{{ID: "source"}},
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}

	result, err := tool.Execute(context.Background(), `{"action":"update"}`)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if got := atomic.LoadInt32(&callbackCalls); got != 0 {
		t.Fatalf("callback calls = %d, want 0", got)
	}
}

func TestExecuteInstallCallsSkillsChangedCallback(t *testing.T) {
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })

	skillBody := []byte("---\nname: demo\ndescription: demo\ntools:\n  - name: ping\n    type: shell\n    command: [\"echo\", \"ok\"]\n---\nDemo skill")
	sum := sha256.Sum256(skillBody)
	digest := hex.EncodeToString(sum[:])

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index.json":
			writer.Header().Set("content-type", "application/json")
			_, _ = writer.Write([]byte(`{"publisher":"example","skills":[{"name":"demo","version":"1.0.0","url":"` + serverURL + `/demo.md","sha256":"` + digest + `"}]}`))
		case "/demo.md":
			_, _ = writer.Write(skillBody)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	var callbackCalls int32
	tool := &skillsTool{
		registries: []configs.SkillsRegistry{
			{
				ID:               "source",
				IndexURL:         serverURL + "/index.json",
				IgnoreSignatures: true,
			},
		},
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}
	if _, err := tool.Execute(context.Background(), `{"action":"install","name":"demo"}`); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if got := atomic.LoadInt32(&callbackCalls); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}
