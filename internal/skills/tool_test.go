package skills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		if result != nil {
			t.Errorf("expected nil for invalid JSON, got %v", result)
		}
	})

	t.Run("empty string", func(t *testing.T) {
		result := parseArguments("")
		if result != nil {
			t.Errorf("expected nil for empty string, got %v", result)
		}
	})
}

func TestApplyTemplate(t *testing.T) {
	t.Run("single substitution", func(t *testing.T) {
		result := applyTemplate("hello {{name}}", map[string]interface{}{"name": "world"})
		if result != "hello world" {
			t.Errorf("got %q, want %q", result, "hello world")
		}
	})

	t.Run("multiple substitutions", func(t *testing.T) {
		args := map[string]interface{}{"host": "localhost", "port": float64(8080)}
		result := applyTemplate("{{host}}:{{port}}", args)
		if result != "localhost:8080" {
			t.Errorf("got %q, want %q", result, "localhost:8080")
		}
	})

	t.Run("repeated placeholder", func(t *testing.T) {
		result := applyTemplate("{{x}} and {{x}}", map[string]interface{}{"x": "a"})
		if result != "a and a" {
			t.Errorf("got %q, want %q", result, "a and a")
		}
	})

	t.Run("no placeholders", func(t *testing.T) {
		result := applyTemplate("no placeholders", map[string]interface{}{"key": "value"})
		if result != "no placeholders" {
			t.Errorf("got %q, want %q", result, "no placeholders")
		}
	})

	t.Run("nil args", func(t *testing.T) {
		result := applyTemplate("hello {{name}}", nil)
		if result != "hello {{name}}" {
			t.Errorf("got %q, want template unchanged", result)
		}
	})

	t.Run("missing key left as-is", func(t *testing.T) {
		result := applyTemplate("{{known}} {{unknown}}", map[string]interface{}{"known": "yes"})
		if result != "yes {{unknown}}" {
			t.Errorf("got %q, want %q", result, "yes {{unknown}}")
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
	tool := &ShellTool{definition: ToolDefinition{
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
		tool := &ShellTool{definition: ToolDefinition{
			Name:    "echo_test",
			Type:    "shell",
			Command: []string{"echo", "hello"},
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.TrimSpace(result) != "hello" {
			t.Errorf("got %q, want %q", strings.TrimSpace(result), "hello")
		}
	})

	t.Run("template substitution", func(t *testing.T) {
		tool := &ShellTool{definition: ToolDefinition{
			Name:    "greet",
			Type:    "shell",
			Command: []string{"echo", "hello {{name}}"},
		}}

		result, err := tool.Execute(context.Background(), `{"name":"world"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.TrimSpace(result) != "hello world" {
			t.Errorf("got %q, want %q", strings.TrimSpace(result), "hello world")
		}
	})

	t.Run("default timeout used", func(t *testing.T) {
		tool := &ShellTool{definition: ToolDefinition{
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
		tool := &ShellTool{definition: ToolDefinition{
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
		tool := &ShellTool{definition: ToolDefinition{
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
		tool := &ShellTool{definition: ToolDefinition{
			Name:             "pwd_test",
			Type:             "shell",
			Command:          []string{"pwd"},
			WorkingDirectory: directory,
		}}

		result, err := tool.Execute(context.Background(), "{}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.TrimSpace(result) != directory {
			t.Errorf("got %q, want %q", strings.TrimSpace(result), directory)
		}
	})

	t.Run("output truncation", func(t *testing.T) {
		// Generate output larger than maxResultBytes.
		repeatCount := (maxResultBytes / 10) + 100
		tool := &ShellTool{definition: ToolDefinition{
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
		tool := &ShellTool{definition: ToolDefinition{
			Name:    "stdin_test",
			Type:    "shell",
			Command: []string{"cat"},
		}}

		result, err := tool.Execute(context.Background(), `{"message":"from stdin"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if strings.TrimSpace(result) != `{"message":"from stdin"}` {
			t.Errorf("got %q, want raw arguments echoed", strings.TrimSpace(result))
		}
	})
}

func TestHTTPToolDefinition(t *testing.T) {
	tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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

		tool := &HTTPTool{definition: ToolDefinition{
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
}
