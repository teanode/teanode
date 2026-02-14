package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/provider"
)

const (
	defaultTimeout = 30 // seconds
	maxResultBytes = 50 * 1024
)

// ShellTool implements agent.Tool for shell-type skill tools.
type ShellTool struct {
	definition ToolDef
}

func (self *ShellTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        self.definition.Name,
			Description: self.definition.Description,
			Parameters:  self.definition.Parameters,
		},
	}
}

func (self *ShellTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	arguments := parseArguments(rawArguments)

	// Apply template substitution to command elements.
	commandParts := make([]string, len(self.definition.Command))
	for i, element := range self.definition.Command {
		commandParts[i] = applyTemplate(element, arguments)
	}

	timeout := self.definition.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	log.Debugf("shell exec: %v", commandParts)

	command := exec.CommandContext(ctx, commandParts[0], commandParts[1:]...)
	if self.definition.WorkingDirectory != "" {
		command.Dir = self.definition.WorkingDirectory
	}
	command.Stdin = strings.NewReader(rawArguments)

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			log.Debugf("shell stderr: %s", stderrStr)
		}
		return "", fmt.Errorf("command failed: %v\n%s", err, stderrStr)
	}

	if stderrStr := stderr.String(); stderrStr != "" {
		log.Debugf("shell stderr: %s", stderrStr)
	}

	return truncate(stdout.String(), maxResultBytes), nil
}

// HTTPTool implements agent.Tool for http-type skill tools.
type HTTPTool struct {
	definition ToolDef
}

func (self *HTTPTool) Definition() provider.ToolDef {
	return provider.ToolDef{
		Type: "function",
		Function: provider.FunctionSpec{
			Name:        self.definition.Name,
			Description: self.definition.Description,
			Parameters:  self.definition.Parameters,
		},
	}
}

func (self *HTTPTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	arguments := parseArguments(rawArguments)

	targetUrl := applyTemplate(self.definition.URL, arguments)
	body := applyTemplate(self.definition.Body, arguments)

	method := self.definition.Method
	if method == "" {
		method = "GET"
	}

	timeout := self.definition.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	log.Debugf("http %s %s", method, targetUrl)

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	request, err := http.NewRequestWithContext(ctx, method, targetUrl, bodyReader)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	for headerName, headerValue := range self.definition.Headers {
		request.Header.Set(headerName, headerValue)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResultBytes+1))
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	result := truncate(string(responseBody), maxResultBytes)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		snippet := result
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return "", fmt.Errorf("HTTP %d: %s", response.StatusCode, snippet)
	}

	return result, nil
}

// parseArguments extracts a map from JSON arguments string.
func parseArguments(arguments string) map[string]interface{} {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return nil
	}
	return parsed
}

// applyTemplate replaces {{varName}} placeholders with values from args.
func applyTemplate(template string, args map[string]interface{}) string {
	if args == nil {
		return template
	}
	result := template
	for key, value := range args {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", value))
	}
	return result
}

// truncate limits a string to maximumLength bytes.
func truncate(text string, maximumLength int) string {
	if len(text) <= maximumLength {
		return text
	}
	return text[:maximumLength] + "\n... (truncated)"
}
