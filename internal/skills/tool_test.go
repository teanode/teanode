package skills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
)

func TestParseArguments(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		result := parseArguments(`{"name":"alice","count":3}`)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["name"] != "alice" {
			t.Errorf("name = %v, want alice", result["name"])
		}
		// JSON numbers decode as float64.
		if result["count"] != float64(3) {
			t.Errorf("count = %v, want 3", result["count"])
		}
	})

	t.Run("empty JSON object", func(t *testing.T) {
		result := parseArguments("{}")
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result) != 0 {
			t.Errorf("expected empty map, got %v", result)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		result := parseArguments("not json")
		if result == nil || len(result) != 0 {
			t.Errorf("expected empty map for invalid JSON, got %v", result)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		result := parseArguments("")
		if result == nil || len(result) != 0 {
			t.Errorf("expected empty map for empty string, got %v", result)
		}
	})
}

func TestApplyTemplate(t *testing.T) {
	t.Run("single substitution", func(t *testing.T) {
		result := applyTemplate(context.Background(), "hello {{name}}", map[string]interface{}{"name": "world"})
		if result != "hello world" {
			t.Errorf("got %q, want %q", result, "hello world")
		}
	})

	t.Run("multiple substitutions", func(t *testing.T) {
		args := map[string]interface{}{"host": "localhost", "port": float64(8080)}
		result := applyTemplate(context.Background(), "{{host}}:{{port}}", args)
		if result != "localhost:8080" {
			t.Errorf("got %q, want %q", result, "localhost:8080")
		}
	})

	t.Run("repeated placeholder", func(t *testing.T) {
		result := applyTemplate(context.Background(), "{{x}} and {{x}}", map[string]interface{}{"x": "a"})
		if result != "a and a" {
			t.Errorf("got %q, want %q", result, "a and a")
		}
	})

	t.Run("no placeholders", func(t *testing.T) {
		result := applyTemplate(context.Background(), "no placeholders", map[string]interface{}{"key": "value"})
		if result != "no placeholders" {
			t.Errorf("got %q, want %q", result, "no placeholders")
		}
	})

	t.Run("nil args", func(t *testing.T) {
		result := applyTemplate(context.Background(), "hello {{name}}", nil)
		if result != "hello {{name}}" {
			t.Errorf("got %q, want template unchanged", result)
		}
	})

	t.Run("missing key left as-is", func(t *testing.T) {
		result := applyTemplate(context.Background(), "{{known}} {{unknown}}", map[string]interface{}{"known": "yes"})
		if result != "yes {{unknown}}" {
			t.Errorf("got %q, want %q", result, "yes {{unknown}}")
		}
	})

	t.Run("dot path substitution", func(t *testing.T) {
		result := applyTemplate(context.Background(), "id={{steps.fetch}}", map[string]interface{}{
			"steps": map[string]interface{}{
				"fetch": "abc123",
			},
		})
		if result != "id=abc123" {
			t.Errorf("got %q, want %q", result, "id=abc123")
		}
	})

	t.Run("filters", func(t *testing.T) {
		args := map[string]interface{}{
			"query": "hello world",
			"tags":  []interface{}{"a", "b"},
		}
		got := applyTemplate(context.Background(), "{{query|urlencode}}|{{tags|join:;}}|{{missing|default:na}}", args)
		want := "hello+world|a;b|na"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("secret and env resolution", func(t *testing.T) {
		ctx := contextWithSecrets(t, map[string]string{
			"API_TOKEN": "from-config",
		})
		t.Setenv("ONLY_ENV", "from-env")
		got := applyTemplate(ctx, "{{secret:API_TOKEN}}|{{secret:ONLY_ENV}}|{{env:ONLY_ENV}}", map[string]interface{}{})
		want := "from-config|from-env|from-env"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestTruncate(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		result := truncate("hello", 100)
		if result != "hello" {
			t.Errorf("got %q, want %q", result, "hello")
		}
	})

	t.Run("exact length unchanged", func(t *testing.T) {
		result := truncate("12345", 5)
		if result != "12345" {
			t.Errorf("got %q, want %q", result, "12345")
		}
	})

	t.Run("long text truncated", func(t *testing.T) {
		result := truncate("1234567890", 5)
		if result != "12345\n... (truncated)" {
			t.Errorf("got %q, want %q", result, "12345\n... (truncated)")
		}
	})

	t.Run("empty text", func(t *testing.T) {
		result := truncate("", 10)
		if result != "" {
			t.Errorf("got %q, want empty", result)
		}
	})
}

func TestShellToolDefinition(t *testing.T) {
	tool := &ShellTool{definition: models.SkillTool{
		Name:        "list_files",
		Description: "List directory contents",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}}

	definition := tool.Definition()
	if definition.Type != "function" {
		t.Errorf("type = %q, want %q", definition.Type, "function")
	}
	if definition.Function.Name != "list_files" {
		t.Errorf("name = %q, want %q", definition.Function.Name, "list_files")
	}
	if definition.Function.Description != "List directory contents" {
		t.Errorf("description = %q, want %q", definition.Function.Description, "List directory contents")
	}
}

func TestShellToolExecute(t *testing.T) {
	t.Run("simple echo", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "echo_test",
			Type:    "shell",
			Command: []string{"echo", "hello"},
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "hello" {
			t.Errorf("got %q, want %q", result, "hello")
		}
	})

	t.Run("template substitution", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "greet",
			Type:    "shell",
			Command: []string{"echo", "hello {{name}}"},
		}}

		result, err := tool.Execute(context.Background(), `{"name":"world"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "hello world" {
			t.Errorf("got %q, want %q", result, "hello world")
		}
	})

	t.Run("default timeout used", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "fast",
			Type:    "shell",
			Command: []string{"true"},
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("custom timeout", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "fast",
			Type:    "shell",
			Command: []string{"true"},
			Timeout: 5,
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("command failure returns error", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "fail",
			Type:    "shell",
			Command: []string{"false"},
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err == nil {
			t.Fatal("expected error for failing command")
		}
	})

	t.Run("working directory", func(t *testing.T) {
		directory := t.TempDir()
		tool := &ShellTool{definition: models.SkillTool{
			Name:             "pwd_test",
			Type:             "shell",
			Command:          []string{"pwd"},
			WorkingDirectory: directory,
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != directory {
			t.Errorf("got %q, want %q", result, directory)
		}
	})

	t.Run("output truncation", func(t *testing.T) {
		// Generate output larger than maxResultBytes.
		repeatCount := (maxResultBytes / 10) + 100
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "big_output",
			Type:    "shell",
			Command: []string{"sh", "-c", fmt.Sprintf("yes 'aaaaaaaaaa' | head -n %d", repeatCount)},
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(result, "... (truncated)") {
			t.Errorf("expected truncated output, got suffix %q", result[len(result)-30:])
		}
	})

	t.Run("stdin receives raw arguments", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "stdin_test",
			Type:    "shell",
			Command: []string{"cat"},
		}}

		result, err := tool.Execute(context.Background(), `{"message":"from stdin"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != `{"message":"from stdin"}` {
			t.Errorf("got %q, want raw arguments echoed", result)
		}
	})

	t.Run("required parameters enforced", func(t *testing.T) {
		tool := &ShellTool{definition: models.SkillTool{
			Name:    "required_test",
			Type:    "shell",
			Command: []string{"echo", "{{path}}"},
			Parameters: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"path"},
			},
		}}
		_, err := tool.Execute(context.Background(), `{}`)
		if err == nil || !strings.Contains(err.Error(), "missing required parameter: path") {
			t.Fatalf("expected required parameter error, got: %v", err)
		}
	})
}

func TestHTTPToolDefinition(t *testing.T) {
	tool := &HTTPTool{definition: models.SkillTool{
		Name:        "fetch_data",
		Description: "Fetch data from API",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}}

	definition := tool.Definition()
	if definition.Type != "function" {
		t.Errorf("type = %q, want %q", definition.Type, "function")
	}
	if definition.Function.Name != "fetch_data" {
		t.Errorf("name = %q, want %q", definition.Function.Name, "fetch_data")
	}
	if definition.Function.Description != "Fetch data from API" {
		t.Errorf("description = %q, want %q", definition.Function.Description, "Fetch data from API")
	}
}

func TestHTTPToolExecute(t *testing.T) {
	t.Run("GET request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != "GET" {
				t.Errorf("method = %q, want GET", request.Method)
			}
			writer.Write([]byte(`{"status":"ok"}`))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name:   "get_test",
			Type:   "http",
			Method: "GET",
			URL:    server.URL,
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != `{"status":"ok"}` {
			t.Errorf("got %q, want %q", result, `{"status":"ok"}`)
		}
	})

	t.Run("default method is GET", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != "GET" {
				t.Errorf("method = %q, want GET", request.Method)
			}
			writer.Write([]byte("ok"))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name: "default_method",
			Type: "http",
			URL:  server.URL,
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("POST with body template", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method != "POST" {
				t.Errorf("method = %q, want POST", request.Method)
			}
			body := make([]byte, 1024)
			count, _ := request.Body.Read(body)
			bodyString := string(body[:count])
			if bodyString != `{"query":"test search"}` {
				t.Errorf("body = %q", bodyString)
			}
			writer.Write([]byte("received"))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name:   "post_test",
			Type:   "http",
			Method: "POST",
			URL:    server.URL,
			Body:   `{"query":"{{query}}"}`,
		}}

		result, err := tool.Execute(context.Background(), `{"query":"test search"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "received" {
			t.Errorf("got %q, want %q", result, "received")
		}
	})

	t.Run("URL template substitution", func(t *testing.T) {
		var receivedPath string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			receivedPath = request.URL.Path
			writer.Write([]byte("ok"))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name: "url_template",
			Type: "http",
			URL:  server.URL + "/items/{{itemId}}",
		}}

		_, err := tool.Execute(context.Background(), `{"itemId":"42"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if receivedPath != "/items/42" {
			t.Errorf("path = %q, want %q", receivedPath, "/items/42")
		}
	})

	t.Run("custom headers", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Header.Get("X-Api-Key") != "secret123" {
				t.Errorf("X-Api-Key = %q, want %q", request.Header.Get("X-Api-Key"), "secret123")
			}
			if request.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q, want %q", request.Header.Get("Content-Type"), "application/json")
			}
			writer.Write([]byte("ok"))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name: "headers_test",
			Type: "http",
			URL:  server.URL,
			Headers: map[string]string{
				"X-Api-Key":    "secret123",
				"Content-Type": "application/json",
			},
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-2xx status returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusNotFound)
			writer.Write([]byte("not found"))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name: "error_test",
			Type: "http",
			URL:  server.URL,
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err == nil {
			t.Fatal("expected error for 404 response")
		}
		if !strings.Contains(err.Error(), "HTTP 404") {
			t.Errorf("error = %q, want HTTP 404 mention", err.Error())
		}
	})

	t.Run("500 status returns error with body snippet", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusInternalServerError)
			writer.Write([]byte("internal server error"))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name: "server_error",
			Type: "http",
			URL:  server.URL,
		}}

		_, err := tool.Execute(context.Background(), "{}")
		if err == nil {
			t.Fatal("expected error for 500 response")
		}
		if !strings.Contains(err.Error(), "internal server error") {
			t.Errorf("error = %q, want body snippet included", err.Error())
		}
	})

	t.Run("response truncation", func(t *testing.T) {
		bigBody := strings.Repeat("x", maxResultBytes+1000)
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Write([]byte(bigBody))
		}))
		defer server.Close()

		tool := &HTTPTool{definition: models.SkillTool{
			Name: "big_response",
			Type: "http",
			URL:  server.URL,
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(result, "... (truncated)") {
			t.Error("expected truncation suffix")
		}
	})

	t.Run("auth profile bearer", func(t *testing.T) {
		ctx := contextWithSecrets(t, map[string]string{
			"AUTH_TOKEN": "top-secret",
		})
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Header.Get("Authorization") != "Bearer top-secret" {
				t.Fatalf("unexpected Authorization header: %q", request.Header.Get("Authorization"))
			}
			writer.Write([]byte("ok"))
		}))
		defer server.Close()

		tool := &HTTPTool{
			definition: models.SkillTool{
				Name: "auth_test",
				Type: "http",
				URL:  server.URL,
				Auth: "main",
			},
			httpAuthProfiles: map[string]models.SkillAuthenticationProfiles{
				"main": {
					Type:  "bearer",
					Token: "{{secret:AUTH_TOKEN}}",
				},
			},
		}

		if _, err := tool.Execute(ctx, "{}"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func contextWithSecrets(t *testing.T, secrets map[string]string) context.Context {
	t.Helper()
	openedStore := setupSkillStore(t)
	ctx := store.ContextWithStore(context.Background(), openedStore)
	transactionError := openedStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		_, modifyError := transaction.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			secretConfigurations := make([]*models.SecretConfiguration, 0, len(secrets))
			for key, value := range secrets {
				keyCopy := key
				valueCopy := value
				secretConfigurations = append(secretConfigurations, &models.SecretConfiguration{
					Key:   &keyCopy,
					Value: &valueCopy,
				})
			}
			configuration.Secrets = &secretConfigurations
			return nil
		}, nil)
		return modifyError
	})
	if transactionError != nil {
		t.Fatalf("storing secrets in configuration: %v", transactionError)
	}
	return ctx
}

func TestWorkflowToolExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("world"))
	}))
	defer server.Close()

	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "multi_action",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name: "fetch",
				Type: "http",
				URL:  server.URL,
			},
			{
				Name:    "compose",
				Type:    "shell",
				Command: []string{"echo", "hello {{steps.fetch}}"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "\"name\":\"fetch\"") {
		t.Fatalf("missing fetch step in result: %s", result)
	}
	if !strings.Contains(result, "\"name\":\"compose\"") {
		t.Fatalf("missing compose step in result: %s", result)
	}
	if !strings.Contains(result, "hello world") {
		t.Fatalf("missing composed output in result: %s", result)
	}
}

func TestWorkflowToolConditionAndContinueOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		writer.Write([]byte("nope"))
	}))
	defer server.Close()

	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "conditional_flow",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:    "gate",
				Type:    "shell",
				Command: []string{"echo", "run"},
			},
			{
				Name:    "skipped",
				Type:    "shell",
				If:      "false",
				Command: []string{"echo", "should_not_run"},
			},
			{
				Name:    "fails",
				Type:    "http",
				URL:     server.URL,
				OnError: "continue",
			},
			{
				Name:    "after_error",
				Type:    "shell",
				Command: []string{"echo", "ok"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"name":"skipped","type":"shell","status":"skipped"`) {
		t.Fatalf("expected skipped step in result: %s", result)
	}
	if !strings.Contains(result, `"name":"fails","type":"http","status":"error"`) {
		t.Fatalf("expected error step in result: %s", result)
	}
	if !strings.Contains(result, `"name":"after_error","type":"shell","status":"ok"`) {
		t.Fatalf("expected after_error step to continue: %s", result)
	}
}

func TestWorkflowToolConditionMissingPathIsFalse(t *testing.T) {
	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "conditional_missing_path",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:    "guarded",
				Type:    "shell",
				If:      "missing.path",
				Command: []string{"echo", "should_not_run"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"name":"guarded","type":"shell","status":"skipped"`) {
		t.Fatalf("expected missing path condition to skip step: %s", result)
	}
}

func TestWorkflowToolConditionComparison(t *testing.T) {
	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "conditional_comparison",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:    "match",
				Type:    "shell",
				If:      "mode == \"prod\"",
				Command: []string{"echo", "matched"},
			},
			{
				Name:    "missing_is_null",
				Type:    "shell",
				If:      "missing.value == null",
				Command: []string{"echo", "null_match"},
			},
			{
				Name:    "mismatch",
				Type:    "shell",
				If:      "mode != \"prod\"",
				Command: []string{"echo", "should_not_run"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), `{"mode":"prod"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"name":"match","type":"shell","status":"ok"`) {
		t.Fatalf("expected equality condition to run step: %s", result)
	}
	if !strings.Contains(result, `"name":"missing_is_null","type":"shell","status":"ok"`) {
		t.Fatalf("expected null comparison to run step: %s", result)
	}
	if !strings.Contains(result, `"name":"mismatch","type":"shell","status":"skipped"`) {
		t.Fatalf("expected inequality mismatch to skip step: %s", result)
	}
}

func TestWorkflowToolJSONResultReuse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`{"id":"abc-123"}`))
	}))
	defer server.Close()

	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "json_reuse",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:   "fetch",
				Type:   "http",
				URL:    server.URL,
				Result: "json",
			},
			{
				Name:    "use",
				Type:    "shell",
				Command: []string{"echo", "id={{steps.fetch.id}}"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "id=abc-123") {
		t.Fatalf("expected nested json field interpolation: %s", result)
	}
}

func TestWorkflowToolJSONSelectAndExtract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`{"data":{"id":"x1","status":"ok"}}`))
	}))
	defer server.Close()

	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "json_select",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:    "fetch",
				Type:    "http",
				URL:     server.URL,
				Result:  "json",
				Extract: "data",
				Select: map[string]string{
					"id": "id",
				},
			},
			{
				Name:    "use",
				Type:    "shell",
				Command: []string{"echo", "{{steps.fetch.id}}"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"name":"fetch"`) {
		t.Fatalf("missing fetch step in result: %s", result)
	}
	if !strings.Contains(result, "x1") {
		t.Fatalf("expected selected/extracted value to flow through workflow: %s", result)
	}
}

func TestWorkflowForEachSwitchAndFinally(t *testing.T) {
	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "control_flow",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:    "init",
				Type:    "shell",
				Command: []string{"echo", `[1,2,3]`},
				Result:  "json",
				SaveAs:  "numbers",
			},
			{
				Name:    "loop",
				Type:    "forEach",
				ForEach: "steps.numbers",
				As:      "num",
				Steps: []*models.SkillAction{
					{
						Name:   "route",
						Type:   "switch",
						Switch: "num",
						Cases: []*models.SkillCase{
							{
								Match: "2",
								Steps: []*models.SkillAction{
									{Name: "mark_two", Type: "shell", Command: []string{"echo", "two-{{num}}"}},
								},
							},
						},
						Default: []*models.SkillAction{
							{Name: "mark_other", Type: "shell", Command: []string{"echo", "other-{{num}}"}},
						},
					},
				},
			},
		},
		Finally: []*models.SkillAction{
			{
				Name:    "cleanup",
				Type:    "shell",
				Command: []string{"echo", "done"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "loop[0].route.mark_other") {
		t.Fatalf("missing forEach/switch default path: %s", result)
	}
	if !strings.Contains(result, "loop[1].route.mark_two") {
		t.Fatalf("missing switch case path: %s", result)
	}
	if !strings.Contains(result, "finally.cleanup") {
		t.Fatalf("missing finally path: %s", result)
	}
}

func TestWorkflowForEachRestoresAliasIndex(t *testing.T) {
	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "for_each_alias_restore",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{
				Name:    "loop",
				Type:    "forEach",
				ForEach: "items",
				As:      "item",
				Steps: []*models.SkillAction{
					{
						Name:    "work",
						Type:    "shell",
						Command: []string{"echo", "{{itemIndex}}"},
					},
				},
			},
			{
				Name:    "after",
				Type:    "shell",
				Command: []string{"echo", "{{itemIndex}}"},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), `{"items":[1,2],"itemIndex":"seed"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"name":"after","type":"shell","status":"ok","attempts":1`) {
		t.Fatalf("missing after step: %s", result)
	}
	if !strings.Contains(result, `"output":"seed"`) {
		t.Fatalf("expected alias index to restore original value: %s", result)
	}
}

func TestWorkflowActionRouting(t *testing.T) {
	tool := &WorkflowTool{definition: models.SkillTool{
		Name:        "router",
		Type:        "workflow",
		ActionField: "op",
		Actions: map[string][]*models.SkillAction{
			"ping": {
				{Name: "echo", Type: "shell", Command: []string{"echo", "pong"}},
			},
		},
	}}

	result, err := tool.Execute(context.Background(), `{"op":"ping"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "pong") {
		t.Fatalf("missing routed action output: %s", result)
	}

	if _, err := tool.Execute(context.Background(), `{"op":"missing"}`); err == nil {
		t.Fatal("expected unknown action error")
	}
}

func TestToolOutputSchemaValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte(`{"name":"alice"}`))
	}))
	defer server.Close()

	t.Run("pass", func(t *testing.T) {
		tool := &HTTPTool{definition: models.SkillTool{
			Name:   "schema_pass",
			Type:   "http",
			URL:    server.URL,
			Result: "json",
			OutputSchema: map[string]interface{}{
				"type": "object",
				"required": []interface{}{
					"name",
				},
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		}}
		_, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fail", func(t *testing.T) {
		tool := &HTTPTool{definition: models.SkillTool{
			Name:   "schema_fail",
			Type:   "http",
			URL:    server.URL,
			Result: "json",
			OutputSchema: map[string]interface{}{
				"type": "object",
				"required": []interface{}{
					"id",
				},
			},
		}}
		_, err := tool.Execute(context.Background(), "{}")
		if err == nil || !strings.Contains(err.Error(), "missing required field") {
			t.Fatalf("expected output schema validation error, got: %v", err)
		}
	})
}

func TestShellSkillToolsDeniedForNonAdmin(t *testing.T) {
	tool := &ShellTool{definition: models.SkillTool{
		Name:    "echo",
		Type:    "shell",
		Command: []string{"echo", "ok"},
	}}
	nonAdminContext := models.ContextWithUserSessionToken(context.Background(), &models.User{ID: "non-admin"}, nil, nil)
	_, err := tool.Execute(nonAdminContext, "{}")
	if err == nil || !strings.Contains(err.Error(), "admin access required") {
		t.Fatalf("expected admin access required error, got: %v", err)
	}
}

func TestWorkflowShellStepsDeniedForNonAdmin(t *testing.T) {
	tool := &WorkflowTool{definition: models.SkillTool{
		Name: "workflow_shell",
		Type: "workflow",
		Steps: []*models.SkillAction{
			{Name: "echo", Type: "shell", Command: []string{"echo", "ok"}},
		},
	}}
	nonAdminContext := models.ContextWithUserSessionToken(context.Background(), &models.User{ID: "non-admin"}, nil, nil)
	_, err := tool.Execute(nonAdminContext, "{}")
	if err == nil || !strings.Contains(err.Error(), "admin access required") {
		t.Fatalf("expected admin access required error, got: %v", err)
	}
}
