package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/providers"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/cmdexec"
)

const (
	defaultTimeout = 30 // seconds
	maxResultBytes = 50 * 1024
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

// ShellTool implements agent.Tool for shell-type skill tools.
type ShellTool struct {
	definition models.SkillTool
}

func (self *ShellTool) Definition() providers.ToolDefinition {
	return toolDefinition(self.definition)
}

func (self *ShellTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	user := models.UserFromContext(ctx)
	if user == nil || !user.GetAdmin() {
		return "", fmt.Errorf("admin access required for shell skill tool")
	}
	arguments := parseArguments(rawArguments)
	if err := validateRequiredArguments(self.definition.Parameters, arguments); err != nil {
		return "", err
	}
	action := actionFromTool(self.definition)
	output, err := executeShellAction(ctx, action, arguments, rawArguments)
	if err != nil {
		return "", err
	}
	stored, err := processActionOutput(action, output, "tool")
	if err != nil {
		return "", err
	}
	return serializeToolResult(stored), nil
}

// HTTPTool implements agent.Tool for http-type skill tools.
type HTTPTool struct {
	definition             models.SkillTool
	authenticationProfiles map[string]models.SkillAuthenticationProfiles
}

func (self *HTTPTool) Definition() providers.ToolDefinition {
	return toolDefinition(self.definition)
}

func (self *HTTPTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	arguments := parseArguments(rawArguments)
	if err := validateRequiredArguments(self.definition.Parameters, arguments); err != nil {
		return "", err
	}
	action := actionFromTool(self.definition)
	output, err := executeHTTPAction(ctx, action, arguments, self.authenticationProfiles)
	if err != nil {
		return "", err
	}
	stored, err := processActionOutput(action, output, "tool")
	if err != nil {
		return "", err
	}
	return serializeToolResult(stored), nil
}

// WorkflowTool implements agent.Tool for workflow-type skill tools.
type WorkflowTool struct {
	definition             models.SkillTool
	authenticationProfiles map[string]models.SkillAuthenticationProfiles
}

func (self *WorkflowTool) Definition() providers.ToolDefinition {
	return toolDefinition(self.definition)
}

func (self *WorkflowTool) Execute(ctx context.Context, rawArguments string) (string, error) {
	arguments := parseArguments(rawArguments)
	if err := validateRequiredArguments(self.definition.Parameters, arguments); err != nil {
		return "", err
	}

	contextData := map[string]interface{}{}
	for key, value := range arguments {
		contextData[key] = value
	}
	contextData["steps"] = map[string]interface{}{}

	results := []workflowStepResult{}

	mainSteps := self.definition.Steps
	if len(self.definition.Actions) > 0 {
		actionField := self.definition.ActionField
		if actionField == "" {
			actionField = "action"
		}
		actionName := fmt.Sprintf("%v", contextData[actionField])
		if actionName == "" {
			return "", fmt.Errorf("missing required action selector: %s", actionField)
		}
		selectedSteps, ok := self.definition.Actions[actionName]
		if !ok {
			return "", fmt.Errorf("unknown action %q", actionName)
		}
		mainSteps = selectedSteps
	}

	mainOutput, mainError := executeWorkflowSteps(ctx, mainSteps, contextData, &results, "", self.authenticationProfiles)
	finallyOutput, finallyError := executeWorkflowSteps(ctx, self.definition.Finally, contextData, &results, "finally.", self.authenticationProfiles)
	if finallyOutput != nil {
		contextData["lastFinally"] = finallyOutput
	}

	if mainError != nil {
		if finallyError != nil {
			return "", fmt.Errorf("workflow failed: %v (finally failed: %v)", mainError, finallyError)
		}
		return "", mainError
	}
	if finallyError != nil {
		return "", fmt.Errorf("workflow finally failed: %w", finallyError)
	}

	if err := validateOutputSchema(mainOutput, self.definition.OutputSchema); err != nil {
		return "", fmt.Errorf("workflow output schema validation failed: %w", err)
	}

	response, _ := json.Marshal(map[string]interface{}{
		"steps":   results,
		"output":  mainOutput,
		"context": contextData["steps"],
	})
	return string(response), nil
}

type workflowStepResult struct {
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Status     string      `json:"status"`
	Attempts   int         `json:"attempts"`
	DurationMs int64       `json:"durationMs"`
	Output     interface{} `json:"output,omitempty"`
	Error      string      `json:"error,omitempty"`
}

func executeWorkflowSteps(ctx context.Context, steps []*models.SkillAction, contextData map[string]interface{}, results *[]workflowStepResult, namePrefix string, authenticationProfiles map[string]models.SkillAuthenticationProfiles) (interface{}, error) {
	var lastOutput interface{}
	for index, step := range steps {
		output, err := executeWorkflowStep(ctx, step, index, contextData, results, namePrefix, authenticationProfiles)
		if err != nil {
			return lastOutput, err
		}
		if output != nil {
			lastOutput = output
			contextData["last"] = output
		}
	}
	return lastOutput, nil
}

func executeWorkflowStep(ctx context.Context, step *models.SkillAction, stepIndex int, contextData map[string]interface{}, results *[]workflowStepResult, namePrefix string, authenticationProfiles map[string]models.SkillAuthenticationProfiles) (interface{}, error) {
	stepName := step.Name
	if stepName == "" {
		stepName = fmt.Sprintf("step%d", stepIndex+1)
	}
	fullName := namePrefix + stepName

	if !shouldRunStep(ctx, step, contextData) {
		*results = append(*results, workflowStepResult{
			Name:   fullName,
			Type:   string(step.Type),
			Status: "skipped",
		})
		return nil, nil
	}

	switch step.Type {
	case models.SkillActionTypeForEach:
		return executeForEachStep(ctx, step, fullName, contextData, results, authenticationProfiles)
	case models.SkillActionTypeSwitch:
		return executeSwitchStep(ctx, step, fullName, contextData, results, authenticationProfiles)
	case models.SkillActionTypeShell, models.SkillActionTypeHTTP:
		return executeActionStep(ctx, step, fullName, contextData, results, authenticationProfiles)
	default:
		return nil, fmt.Errorf("unknown workflow step type %q", step.Type)
	}
}

func executeActionStep(ctx context.Context, step *models.SkillAction, fullName string, contextData map[string]interface{}, results *[]workflowStepResult, authenticationProfiles map[string]models.SkillAuthenticationProfiles) (interface{}, error) {
	startedAt := time.Now()
	maxAttempts := step.Retries + 1
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var (
		rawOutput string
		err       error
		attempts  int
	)
	for attempts = 1; attempts <= maxAttempts; attempts++ {
		switch step.Type {
		case models.SkillActionTypeShell:
			user := models.UserFromContext(ctx)
			if user == nil || !user.GetAdmin() {
				return nil, fmt.Errorf("admin access required for shell skill actions")
			}
			workflowInput, _ := json.Marshal(contextData)
			rawOutput, err = executeShellAction(ctx, *step, contextData, string(workflowInput))
		case models.SkillActionTypeHTTP:
			rawOutput, err = executeHTTPAction(ctx, *step, contextData, authenticationProfiles)
		}
		if err == nil {
			break
		}
		if attempts < maxAttempts && step.RetryDelay > 0 {
			time.Sleep(time.Duration(step.RetryDelay) * time.Millisecond)
		}
	}

	if err != nil {
		if step.OnError == models.SkillErrorPolicyContinue {
			recordStepOutput(contextData, step, fullName, map[string]interface{}{"error": err.Error()})
			contextData["lastError"] = err.Error()
			*results = append(*results, workflowStepResult{
				Name:       fullName,
				Type:       string(step.Type),
				Status:     "error",
				Attempts:   attempts,
				DurationMs: time.Since(startedAt).Milliseconds(),
				Error:      err.Error(),
			})
			return nil, nil
		}
		return nil, fmt.Errorf("workflow step %s failed: %w", fullName, err)
	}

	storedOutput, processErr := processActionOutput(*step, rawOutput, fullName)
	if processErr != nil {
		return nil, processErr
	}
	recordStepOutput(contextData, step, fullName, storedOutput)
	*results = append(*results, workflowStepResult{
		Name:       fullName,
		Type:       string(step.Type),
		Status:     "ok",
		Attempts:   attempts,
		DurationMs: time.Since(startedAt).Milliseconds(),
		Output:     storedOutput,
	})
	return storedOutput, nil
}

func executeForEachStep(ctx context.Context, step *models.SkillAction, fullName string, contextData map[string]interface{}, results *[]workflowStepResult, authenticationProfiles map[string]models.SkillAuthenticationProfiles) (interface{}, error) {
	startedAt := time.Now()
	itemsRaw, ok := resolveTemplateValue(contextData, step.ForEach)
	if !ok {
		return nil, fmt.Errorf("workflow step %s forEach source not found: %s", fullName, step.ForEach)
	}
	items, ok := toInterfaceSlice(itemsRaw)
	if !ok {
		return nil, fmt.Errorf("workflow step %s forEach source is not an array", fullName)
	}

	alias := step.As
	if alias == "" {
		alias = "item"
	}
	originalAlias, hadAlias := contextData[alias]
	aliasIndexKey := alias + "Index"
	originalAliasIndex, hadAliasIndex := contextData[aliasIndexKey]
	defer func() {
		if hadAlias {
			contextData[alias] = originalAlias
		} else {
			delete(contextData, alias)
		}
		if hadAliasIndex {
			contextData[aliasIndexKey] = originalAliasIndex
		} else {
			delete(contextData, aliasIndexKey)
		}
	}()

	collected := make([]interface{}, 0, len(items))
	for index, item := range items {
		contextData[alias] = item
		contextData[aliasIndexKey] = index
		output, err := executeWorkflowSteps(ctx, step.Steps, contextData, results, fullName+fmt.Sprintf("[%d].", index), authenticationProfiles)
		if err != nil {
			if step.OnError == models.SkillErrorPolicyContinue {
				collected = append(collected, map[string]interface{}{"error": err.Error()})
				continue
			}
			return nil, err
		}
		collected = append(collected, output)
	}

	recordStepOutput(contextData, step, fullName, collected)
	*results = append(*results, workflowStepResult{
		Name:       fullName,
		Type:       string(step.Type),
		Status:     "ok",
		Attempts:   1,
		DurationMs: time.Since(startedAt).Milliseconds(),
		Output:     collected,
	})
	return collected, nil
}

func executeSwitchStep(ctx context.Context, step *models.SkillAction, fullName string, contextData map[string]interface{}, results *[]workflowStepResult, authenticationProfiles map[string]models.SkillAuthenticationProfiles) (interface{}, error) {
	startedAt := time.Now()
	value, ok := resolveTemplateValue(contextData, step.Switch)
	if !ok {
		value = applyTemplate(ctx, step.Switch, contextData)
	}
	matchValue := fmt.Sprintf("%v", value)

	var selectedSteps []*models.SkillAction
	for _, switchCase := range step.Cases {
		if switchCase.Match == matchValue {
			selectedSteps = switchCase.Steps
			break
		}
	}
	if selectedSteps == nil {
		selectedSteps = step.Default
	}

	output, err := executeWorkflowSteps(ctx, selectedSteps, contextData, results, fullName+".", authenticationProfiles)
	if err != nil {
		if step.OnError == models.SkillErrorPolicyContinue {
			recordStepOutput(contextData, step, fullName, map[string]interface{}{"error": err.Error()})
			*results = append(*results, workflowStepResult{
				Name:       fullName,
				Type:       string(step.Type),
				Status:     "error",
				Attempts:   1,
				DurationMs: time.Since(startedAt).Milliseconds(),
				Error:      err.Error(),
			})
			return nil, nil
		}
		return nil, err
	}

	recordStepOutput(contextData, step, fullName, output)
	*results = append(*results, workflowStepResult{
		Name:       fullName,
		Type:       string(step.Type),
		Status:     "ok",
		Attempts:   1,
		DurationMs: time.Since(startedAt).Milliseconds(),
		Output:     output,
	})
	return output, nil
}

func recordStepOutput(contextData map[string]interface{}, step *models.SkillAction, stepName string, output interface{}) {
	saveAs := step.SaveAs
	if saveAs == "" {
		saveAs = stepName
	}
	stepsMap, _ := contextData["steps"].(map[string]interface{})
	stepsMap[saveAs] = output
}

func toolDefinition(definition models.SkillTool) providers.ToolDefinition {
	return providers.ToolDefinition{
		Type: "function",
		Function: providers.FunctionSpec{
			Name:        definition.Name,
			Description: definition.Description,
			Parameters:  definition.Parameters,
		},
	}
}

func actionFromTool(definition models.SkillTool) models.SkillAction {
	return models.SkillAction{
		Type:             models.SkillActionType(definition.Type),
		Command:          definition.Command,
		WorkingDirectory: definition.WorkingDirectory,
		Method:           definition.Method,
		URL:              definition.URL,
		Headers:          definition.Headers,
		Body:             definition.Body,
		Timeout:          definition.Timeout,
		Result:           definition.Result,
		Extract:          definition.Extract,
		Select:           definition.Select,
		OutputSchema:     definition.OutputSchema,
		Auth:             definition.Auth,
	}
}

func executeShellAction(ctx context.Context, action models.SkillAction, arguments map[string]interface{}, stdin string) (string, error) {
	commandParts := make([]string, len(action.Command))
	for index, element := range action.Command {
		commandParts[index] = applyTemplate(ctx, element, arguments)
	}
	timeout := action.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	log.Debugf("shell exec: %v", commandParts)
	result, err := cmdexec.Run(runCtx, commandParts[0], commandParts[1:], cmdexec.Options{
		Directory: action.WorkingDirectory,
		Stdin:     stdin,
	})
	if err != nil {
		stderrString := string(result.Stderr)
		if stderrString != "" {
			log.Debugf("shell stderr: %s", stderrString)
		}
		return "", fmt.Errorf("command failed: %v\n%s", err, stderrString)
	}
	if result.ExitCode != 0 {
		stderrString := string(result.Stderr)
		if stderrString != "" {
			log.Debugf("shell stderr: %s", stderrString)
		}
		return "", fmt.Errorf("command failed with exit code %d\n%s", result.ExitCode, stderrString)
	}
	if stderrString := string(result.Stderr); stderrString != "" {
		log.Debugf("shell stderr: %s", stderrString)
	}
	return truncate(strings.TrimRight(string(result.Stdout), "\n"), maxResultBytes), nil
}

func executeHTTPAction(ctx context.Context, action models.SkillAction, arguments map[string]interface{}, authenticationProfiles map[string]models.SkillAuthenticationProfiles) (string, error) {
	targetUrl := applyTemplate(ctx, action.URL, arguments)
	body := applyTemplate(ctx, action.Body, arguments)
	method := action.Method
	if method == "" {
		method = "GET"
	}
	timeout := action.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	log.Debugf("http %s %s", method, targetUrl)

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	request, err := http.NewRequestWithContext(runCtx, method, targetUrl, bodyReader)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	for headerName, headerValue := range action.Headers {
		request.Header.Set(headerName, headerValue)
	}
	if err := applyAuthenticationProfiles(ctx, request, action, arguments, authenticationProfiles); err != nil {
		return "", err
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

func applyAuthenticationProfiles(ctx context.Context, request *http.Request, action models.SkillAction, arguments map[string]interface{}, authenticationProfiles map[string]models.SkillAuthenticationProfiles) error {
	profileName := action.Auth
	if profileName == "" {
		return nil
	}
	profile, ok := authenticationProfiles[profileName]
	if !ok {
		return fmt.Errorf("http auth profile not found: %s", profileName)
	}
	switch profile.Type {
	case models.SkillAuthenticationTypeBearer:
		token := applyTemplate(ctx, profile.Token, arguments)
		prefix := profile.Prefix
		if prefix == "" {
			prefix = "Bearer "
		}
		request.Header.Set("Authorization", prefix+token)
	case models.SkillAuthenticationTypeBasic:
		username := applyTemplate(ctx, profile.Username, arguments)
		password := applyTemplate(ctx, profile.Password, arguments)
		request.SetBasicAuth(username, password)
	case models.SkillAuthenticationTypeAPIKey:
		value := applyTemplate(ctx, profile.Value, arguments)
		if profile.Prefix != "" {
			value = profile.Prefix + value
		}
		if profile.Header != "" {
			request.Header.Set(profile.Header, value)
		}
		if profile.QueryParam != "" {
			query := request.URL.Query()
			query.Set(profile.QueryParam, value)
			request.URL.RawQuery = query.Encode()
		}
	default:
		return fmt.Errorf("unsupported http auth profile type: %s", profile.Type)
	}
	return nil
}

func processActionOutput(action models.SkillAction, rawOutput string, name string) (interface{}, error) {
	storedOutput := interface{}(rawOutput)
	if action.Result == models.SkillResultFormatJSON {
		var parsed interface{}
		if err := json.Unmarshal([]byte(rawOutput), &parsed); err != nil {
			return nil, fmt.Errorf("%s invalid json output: %w", name, err)
		}
		storedOutput = parsed
		if action.Extract != "" {
			selected, ok := resolvePath(storedOutput, action.Extract)
			if !ok {
				return nil, fmt.Errorf("%s extract path not found: %s", name, action.Extract)
			}
			storedOutput = selected
		}
		if len(action.Select) > 0 {
			selectedMap := map[string]interface{}{}
			for outputKey, outputPath := range action.Select {
				selected, ok := resolvePath(storedOutput, outputPath)
				if !ok {
					return nil, fmt.Errorf("%s select path not found: %s", name, outputPath)
				}
				selectedMap[outputKey] = selected
			}
			storedOutput = selectedMap
		}
	}
	if err := validateOutputSchema(storedOutput, action.OutputSchema); err != nil {
		return nil, fmt.Errorf("%s output schema validation failed: %w", name, err)
	}
	return storedOutput, nil
}

func serializeToolResult(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

// parseArguments extracts a map from JSON arguments string.
func parseArguments(arguments string) map[string]interface{} {
	if arguments == "" {
		return map[string]interface{}{}
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return map[string]interface{}{}
	}
	if parsed == nil {
		return map[string]interface{}{}
	}
	return parsed
}

// applyTemplate replaces {{path.to.value}} placeholders with values from args.
// Supported filters: json, urlencode, base64, default:<text>, join:<separator>.
func applyTemplate(ctx context.Context, template string, args map[string]interface{}) string {
	if args == nil {
		return template
	}
	return placeholderPattern.ReplaceAllStringFunc(template, func(match string) string {
		submatches := placeholderPattern.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}
		value, ok := evaluateTemplateExpression(ctx, args, submatches[1])
		if !ok {
			return match
		}
		return fmt.Sprintf("%v", value)
	})
}

func evaluateTemplateExpression(ctx context.Context, values map[string]interface{}, expression string) (interface{}, bool) {
	parts := strings.Split(expression, "|")
	if len(parts) == 0 {
		return nil, false
	}
	basePath := parts[0]
	var (
		value interface{}
		ok    bool
	)
	switch {
	case strings.HasPrefix(basePath, "secret:"):
		value, ok = resolveSecret(ctx, strings.TrimPrefix(basePath, "secret:"))
	case strings.HasPrefix(basePath, "env:"):
		value, ok = resolveEnv(strings.TrimPrefix(basePath, "env:"))
	default:
		value, ok = resolveTemplateValue(values, basePath)
	}
	for _, rawFilter := range parts[1:] {
		name, arg := parseFilter(rawFilter)
		switch name {
		case "default":
			if !ok || isEmptyValue(value) {
				value = arg
				ok = true
			}
		case "json":
			if !ok {
				return nil, false
			}
			data, err := json.Marshal(value)
			if err != nil {
				return nil, false
			}
			value = string(data)
		case "urlencode":
			if !ok {
				return nil, false
			}
			value = url.QueryEscape(fmt.Sprintf("%v", value))
		case "base64":
			if !ok {
				return nil, false
			}
			value = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%v", value)))
		case "join":
			if !ok {
				return nil, false
			}
			separator := ","
			if arg != "" {
				separator = arg
			}
			value = joinValue(value, separator)
		default:
			return nil, false
		}
	}
	return value, ok
}

func parseFilter(raw string) (string, string) {
	filter := raw
	parts := strings.SplitN(filter, ":", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func resolveTemplateValue(values map[string]interface{}, path string) (interface{}, bool) {
	return resolvePath(values, path)
}

func resolvePath(root interface{}, path string) (interface{}, bool) {
	current := root
	for _, part := range strings.Split(path, ".") {
		switch typed := current.(type) {
		case map[string]interface{}:
			next, exists := typed[part]
			if !exists {
				return nil, false
			}
			current = next
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func toInterfaceSlice(value interface{}) ([]interface{}, bool) {
	switch typed := value.(type) {
	case []interface{}:
		return typed, true
	case []string:
		result := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result, true
	default:
		return nil, false
	}
}

func shouldRunStep(ctx context.Context, action *models.SkillAction, contextData map[string]interface{}) bool {
	if action.If == "" {
		return true
	}
	condition := action.If
	if strings.Contains(condition, "==") || strings.Contains(condition, "!=") {
		comparisonValue, ok := evaluateConditionComparison(condition, contextData)
		if !ok {
			return false
		}
		return comparisonValue
	}
	value, ok := resolveConditionValue(ctx, condition, contextData)
	if !ok {
		return false
	}
	return isTruthy(value)
}

func evaluateConditionComparison(condition string, contextData map[string]interface{}) (bool, bool) {
	operator := "=="
	parts := strings.SplitN(condition, "==", 2)
	if len(parts) != 2 {
		operator = "!="
		parts = strings.SplitN(condition, "!=", 2)
	}
	if len(parts) != 2 {
		return false, false
	}
	left, leftOk := resolveConditionOperand(strings.TrimSpace(parts[0]), contextData)
	right, rightOk := resolveConditionOperand(strings.TrimSpace(parts[1]), contextData)
	if !leftOk || !rightOk {
		return false, false
	}
	equal := valuesEqual(left, right)
	if operator == "!=" {
		return !equal, true
	}
	return equal, true
}

func resolveConditionOperand(token string, contextData map[string]interface{}) (interface{}, bool) {
	if token == "" {
		return nil, false
	}
	if literal, ok := parseConditionLiteral(token); ok {
		return literal, true
	}
	if value, ok := resolveTemplateValue(contextData, token); ok {
		return value, true
	}
	if isLikelyPathToken(token) {
		return nil, true
	}
	return nil, false
}

func resolveConditionValue(ctx context.Context, condition string, contextData map[string]interface{}) (interface{}, bool) {
	if literal, ok := parseConditionLiteral(condition); ok {
		return literal, true
	}
	if value, ok := resolveTemplateValue(contextData, condition); ok {
		return value, true
	}
	if strings.Contains(condition, "{{") {
		templated := applyTemplate(ctx, condition, contextData)
		if literal, ok := parseConditionLiteral(templated); ok {
			return literal, true
		}
		if templated != condition && !strings.Contains(templated, "{{") {
			return templated, true
		}
	}
	return nil, false
}

func parseConditionLiteral(token string) (interface{}, bool) {
	switch strings.ToLower(token) {
	case "null":
		return nil, true
	case "true":
		return true, true
	case "false":
		return false, true
	}
	if len(token) >= 2 {
		if (token[0] == '"' && token[len(token)-1] == '"') || (token[0] == '\'' && token[len(token)-1] == '\'') {
			return token[1 : len(token)-1], true
		}
	}
	if number, err := strconv.ParseFloat(token, 64); err == nil {
		return number, true
	}
	return nil, false
}

func isLikelyPathToken(token string) bool {
	if token == "" || strings.Contains(token, " ") {
		return false
	}
	for _, runeValue := range token {
		if (runeValue >= 'a' && runeValue <= 'z') || (runeValue >= 'A' && runeValue <= 'Z') || (runeValue >= '0' && runeValue <= '9') || runeValue == '_' || runeValue == '.' {
			continue
		}
		return false
	}
	return true
}

func valuesEqual(left interface{}, right interface{}) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if leftNumber, ok := toFloat(left); ok {
		rightNumber, rightOk := toFloat(right)
		return rightOk && leftNumber == rightNumber
	}
	return fmt.Sprintf("%v", left) == fmt.Sprintf("%v", right)
}

func toFloat(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func isTruthy(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(typed)
		return normalized != "" && normalized != "0" && normalized != "false" && normalized != "no" && normalized != "off"
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case int32:
		return typed != 0
	default:
		return true
	}
}

func isEmptyValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []interface{}:
		return len(typed) == 0
	case map[string]interface{}:
		return len(typed) == 0
	default:
		return false
	}
}

func joinValue(value interface{}, separator string) string {
	switch typed := value.(type) {
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return strings.Join(parts, separator)
	case []string:
		return strings.Join(typed, separator)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func validateRequiredArguments(parameters interface{}, arguments map[string]interface{}) error {
	parameterMap, ok := parameters.(map[string]interface{})
	if !ok || parameterMap == nil {
		return nil
	}
	rawRequired, exists := parameterMap["required"]
	if !exists {
		return nil
	}
	requiredList, ok := rawRequired.([]interface{})
	if !ok {
		return nil
	}
	for _, rawName := range requiredList {
		name, ok := rawName.(string)
		if !ok || name == "" {
			continue
		}
		value, exists := arguments[name]
		if !exists || isEmptyValue(value) {
			return fmt.Errorf("missing required parameter: %s", name)
		}
	}
	return nil
}

func validateOutputSchema(value interface{}, schema map[string]interface{}) error {
	if len(schema) == 0 {
		return nil
	}
	return validateSchemaNode(value, schema)
}

func validateSchemaNode(value interface{}, schema map[string]interface{}) error {
	rawType, _ := schema["type"].(string)
	switch rawType {
	case "":
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "number":
		switch value.(type) {
		case float64, float32, int, int32, int64:
		default:
			return fmt.Errorf("expected number")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "array":
		arrayValue, ok := value.([]interface{})
		if !ok {
			return fmt.Errorf("expected array")
		}
		if itemSchema, ok := schema["items"].(map[string]interface{}); ok {
			for index, item := range arrayValue {
				if err := validateSchemaNode(item, itemSchema); err != nil {
					return fmt.Errorf("items[%d]: %w", index, err)
				}
			}
		}
	case "object":
		objectValue, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected object")
		}
		if required, ok := schema["required"].([]interface{}); ok {
			for _, requiredKeyRaw := range required {
				requiredKey, _ := requiredKeyRaw.(string)
				if requiredKey == "" {
					continue
				}
				if _, exists := objectValue[requiredKey]; !exists {
					return fmt.Errorf("missing required field %q", requiredKey)
				}
			}
		}
		if properties, ok := schema["properties"].(map[string]interface{}); ok {
			for key, rawPropertySchema := range properties {
				propertySchema, ok := rawPropertySchema.(map[string]interface{})
				if !ok {
					continue
				}
				propertyValue, exists := objectValue[key]
				if !exists {
					continue
				}
				if err := validateSchemaNode(propertyValue, propertySchema); err != nil {
					return fmt.Errorf("%s: %w", key, err)
				}
			}
		}
	default:
		return fmt.Errorf("unsupported schema type %q", rawType)
	}
	return nil
}

// truncate limits a string to maximumLength bytes.
func truncate(text string, maximumLength int) string {
	if len(text) <= maximumLength {
		return text
	}
	return text[:maximumLength] + "\n... (truncated)"
}

func resolveSecret(ctx context.Context, name string) (string, bool) {
	dataStore := store.StoreFromContext(ctx)
	var secretValue string
	var found bool
	_ = dataStore.Transaction(ctx, func(ctx context.Context, transaction store.Transaction) error {
		configuration, err := transaction.GetConfiguration(ctx, nil)
		if err != nil {
			return err
		}
		if configuration.Secrets != nil {
			for _, secret := range *configuration.Secrets {
				key := ""
				if secret.Key != nil {
					key = *secret.Key
				}
				if key == name {
					if secret.Value != nil {
						secretValue = *secret.Value
					}
					found = true
					return nil
				}
			}
		}
		return nil
	})
	if found && secretValue != "" {
		return secretValue, true
	}
	value := os.Getenv(name)
	if value != "" {
		return value, true
	}
	return "", false
}

func resolveEnv(name string) (string, bool) {
	value := os.Getenv(name)
	if value == "" {
		return "", false
	}
	return value, true
}
