package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/teanode/teanode/internal/agents"
	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	storefs "github.com/teanode/teanode/internal/store/fs"
	"github.com/teanode/teanode/internal/util/ptrto"
)

func setupSkillBackend(t *testing.T) store.Store {
	t.Helper()
	directory := t.TempDir()
	configs.SetDirectory(directory)
	openedStore, openError := storefs.Open(storefs.Options{DataDirectory: directory})
	if openError != nil {
		t.Fatalf("opening store backend: %v", openError)
	}
	if migrateError := openedStore.Migrate(); migrateError != nil {
		t.Fatalf("migrating store backend: %v", migrateError)
	}
	t.Cleanup(func() {
		configs.SetDirectory("")
		_ = openedStore.Close()
	})
	return openedStore
}

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
	openedStore := setupSkillBackend(t)
	version := "1.0.0"
	createError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, skillCreateError := transaction.CreateSkill(&models.Skill{
			ID:      "demo",
			Name:    ptrto.Value("demo"),
			Version: &version,
		}, nil)
		return skillCreateError
	})
	if createError != nil {
		t.Fatalf("creating seeded skill: %v", createError)
	}

	var callbackCalls int32
	tool := &skillsTool{
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}
	ctx := store.ContextWithStore(context.Background(), openedStore)
	if _, err := tool.Execute(ctx, `{"action":"uninstall","name":"demo"}`); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if got := atomic.LoadInt32(&callbackCalls); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestExecuteUpdateNoChangesDoesNotCallSkillsChangedCallback(t *testing.T) {
	openedStore := setupSkillBackend(t)

	var callbackCalls int32
	tool := &skillsTool{
		registries: []configs.SkillsRegistry{{ID: "source"}},
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	result, err := tool.Execute(ctx, `{"action":"update"}`)
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
	openedStore := setupSkillBackend(t)

	skillBody := []byte("---\nname: demo\ndescription: demo\ntools:\n  - name: ping\n    type: shell\n    command: [\"echo\", \"ok\"]\n---\nDemo skill")
	sum := sha256.Sum256(skillBody)
	digest := hex.EncodeToString(sum[:])

	var serverUrl string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index.json":
			writer.Header().Set("content-type", "application/json")
			_, _ = writer.Write([]byte(`{"publisher":"example","skills":[{"name":"demo","version":"1.0.0","url":"` + serverUrl + `/demo.md","sha256":"` + digest + `"}]}`))
		case "/demo.md":
			_, _ = writer.Write(skillBody)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	serverUrl = server.URL

	var callbackCalls int32
	tool := &skillsTool{
		registries: []configs.SkillsRegistry{
			{
				ID:               "source",
				IndexURL:         serverUrl + "/index.json",
				IgnoreSignatures: true,
			},
		},
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}
	ctx := store.ContextWithStore(context.Background(), openedStore)
	if _, err := tool.Execute(ctx, `{"action":"install","name":"demo"}`); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if got := atomic.LoadInt32(&callbackCalls); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestExecuteEnableDisableActions(t *testing.T) {
	openedStore := setupSkillBackend(t)
	version := "1.0.0"
	createError := openedStore.Transaction(func(transaction store.Transaction) error {
		_, skillCreateError := transaction.CreateSkill(&models.Skill{
			ID:      "demo",
			Name:    ptrto.Value("demo"),
			Version: &version,
		}, nil)
		return skillCreateError
	})
	if createError != nil {
		t.Fatalf("creating seeded skill: %v", createError)
	}

	var callbackCalls int32
	tool := &skillsTool{
		onSkillsChanged: func() {
			atomic.AddInt32(&callbackCalls, 1)
		},
	}

	ctx := store.ContextWithStore(context.Background(), openedStore)
	disabledRaw, err := tool.Execute(ctx, `{"action":"disable","name":"demo"}`)
	if err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	var disabledPayload map[string]interface{}
	if err := json.Unmarshal([]byte(disabledRaw), &disabledPayload); err != nil {
		t.Fatalf("invalid disable payload: %v", err)
	}
	if disabledPayload["action"] != "set_enabled" {
		t.Fatalf("action = %v, want set_enabled", disabledPayload["action"])
	}
	if disabledPayload["enabled"] != false {
		t.Fatalf("enabled = %v, want false", disabledPayload["enabled"])
	}

	enabledRaw, err := tool.Execute(ctx, `{"action":"enable","name":"demo"}`)
	if err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	var enabledPayload map[string]interface{}
	if err := json.Unmarshal([]byte(enabledRaw), &enabledPayload); err != nil {
		t.Fatalf("invalid enable payload: %v", err)
	}
	if enabledPayload["action"] != "set_enabled" {
		t.Fatalf("action = %v, want set_enabled", enabledPayload["action"])
	}
	if enabledPayload["enabled"] != true {
		t.Fatalf("enabled = %v, want true", enabledPayload["enabled"])
	}

	if got := atomic.LoadInt32(&callbackCalls); got != 2 {
		t.Fatalf("callback calls = %d, want 2", got)
	}
}
