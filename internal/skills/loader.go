package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/version"
	"gopkg.in/yaml.v3"
)

// LoadAll reads all *.md files from skillsDirectory and returns parsed skills.
// Logs warnings for malformed files and continues.
func LoadAll(ctx context.Context, skillsDirectory string) ([]SkillDefinition, error) {
	definitions := make([]SkillDefinition, 0)
	transactionError := store.StoreFromContext(ctx).Transaction(func(transaction store.Transaction) error {
		skills, listError := transaction.ListSkills(nil)
		if listError != nil {
			return listError
		}
		for _, skillModel := range skills {
			if skillModel.Metadata != nil {
				if enabledValue, exists := (*skillModel.Metadata)["enabled"]; exists {
					if enabledBool, ok := enabledValue.(bool); ok && !enabledBool {
						continue
					}
				}
			}
			definition, convertError := skillModelToDefinition(skillModel)
			if convertError != nil {
				log.Warningf("failed to parse skill model %s: %v", skillModel.ID, convertError)
				continue
			}
			if definition.Name == "" {
				log.Warningf("skill %s missing name, skipping", skillModel.ID)
				continue
			}
			if !isRuntimeCompatible(definition.RuntimeMinVersion) {
				log.Warningf("skill %s requires runtime >= %s, skipping", definition.Name, definition.RuntimeMinVersion)
				continue
			}
			var validTools []ToolDefinition
			for _, tool := range definition.Tools {
				if validationError := validateTool(tool); validationError != nil {
					log.Warningf("skill %s: tool %q invalid: %v", definition.Name, tool.Name, validationError)
					continue
				}
				validTools = append(validTools, tool)
			}
			definition.Tools = validTools
			if len(definition.Tools) == 0 {
				log.Warningf("skill %s: no valid tools, skipping", definition.Name)
				continue
			}
			if authValidationError := validateHTTPAuthProfiles(definition.HTTPAuth); authValidationError != nil {
				log.Warningf("skill %s: %v", definition.Name, authValidationError)
				continue
			}
			if authReferenceError := validateSkillAuthReferences(definition); authReferenceError != nil {
				log.Warningf("skill %s: %v", definition.Name, authReferenceError)
				continue
			}
			definitions = append(definitions, definition)
		}
		return nil
	})
	if transactionError != nil {
		return nil, transactionError
	}
	return definitions, nil
}

func skillModelToDefinition(skillModel models.Skill) (SkillDefinition, error) {
	definition := SkillDefinition{}
	if skillModel.Name != nil && strings.TrimSpace(*skillModel.Name) != "" {
		definition.Name = strings.TrimSpace(*skillModel.Name)
	} else if strings.TrimSpace(skillModel.ID) != "" {
		definition.Name = strings.TrimSpace(skillModel.ID)
	}
	definition.Description = strings.TrimSpace(valueOrEmptyString(skillModel.Description))
	definition.RuntimeMinVersion = strings.TrimSpace(valueOrEmptyString(skillModel.RuntimeMinVersion))
	if skillModel.Prompt != nil {
		definition.Prompt = strings.TrimSpace(*skillModel.Prompt)
	}
	if skillModel.HTTPAuth != nil {
		httpAuthData, marshalError := json.Marshal(*skillModel.HTTPAuth)
		if marshalError != nil {
			return SkillDefinition{}, marshalError
		}
		if unmarshalError := json.Unmarshal(httpAuthData, &definition.HTTPAuth); unmarshalError != nil {
			return SkillDefinition{}, unmarshalError
		}
	}
	if skillModel.Tools != nil {
		toolsData, marshalError := json.Marshal(*skillModel.Tools)
		if marshalError != nil {
			return SkillDefinition{}, marshalError
		}
		if unmarshalError := json.Unmarshal(toolsData, &definition.Tools); unmarshalError != nil {
			return SkillDefinition{}, unmarshalError
		}
	}
	if skillModel.Metadata != nil {
		if isLocalValue, exists := (*skillModel.Metadata)["isLocal"]; exists {
			if isLocalBool, ok := isLocalValue.(bool); ok {
				definition.IsLocal = isLocalBool
			}
		}
		if strings.TrimSpace(definition.Description) == "" {
			if descriptionValue, exists := (*skillModel.Metadata)["description"]; exists {
				if descriptionText, ok := descriptionValue.(string); ok {
					definition.Description = strings.TrimSpace(descriptionText)
				}
			}
		}
	}
	return definition, nil
}

func parseSkillMarkdown(data []byte, definition *SkillDefinition) (string, error) {
	content := string(data)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	// Expect: ---\n<yaml>\n---\n<body>
	if !strings.HasPrefix(content, "---\n") {
		return "", fmt.Errorf("missing frontmatter delimiter")
	}

	rest := content[4:] // skip opening "---\n"
	closingIndex := strings.Index(rest, "\n---\n")
	if closingIndex < 0 {
		if strings.HasSuffix(rest, "\n---") {
			closingIndex = len(rest) - 4
		} else {
			return "", fmt.Errorf("missing closing frontmatter delimiter")
		}
	}

	frontmatterYAML := rest[:closingIndex]
	if err := yaml.Unmarshal([]byte(frontmatterYAML), definition); err != nil {
		return "", fmt.Errorf("parsing frontmatter: %w", err)
	}

	body := ""
	bodyStart := closingIndex + 5 // len("\n---\n")
	if bodyStart <= len(rest) {
		body = rest[bodyStart:]
	}
	return body, nil
}

func compareVersions(left string, right string) int {
	// Best-effort semver-ish comparison without external deps.
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}

	for index := 0; index < maxLen; index++ {
		leftPart := "0"
		rightPart := "0"
		if index < len(leftParts) {
			leftPart = leftParts[index]
		}
		if index < len(rightParts) {
			rightPart = rightParts[index]
		}

		leftNumber, leftErr := strconv.Atoi(leftPart)
		rightNumber, rightErr := strconv.Atoi(rightPart)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber > rightNumber {
				return 1
			}
			if leftNumber < rightNumber {
				return -1
			}
		default:
			if leftPart > rightPart {
				return 1
			}
			if leftPart < rightPart {
				return -1
			}
		}
	}
	return 0
}

func validateTool(tool ToolDefinition) error {
	if tool.Name == "" {
		return fmt.Errorf("missing name")
	}
	switch tool.Type {
	case "shell":
		if len(tool.Command) == 0 {
			return fmt.Errorf("shell tool %q has empty command", tool.Name)
		}
	case "http":
		if tool.URL == "" {
			return fmt.Errorf("http tool %q has empty url", tool.Name)
		}
	case "workflow":
		if len(tool.Steps) == 0 && len(tool.Actions) == 0 {
			return fmt.Errorf("workflow tool %q has no steps", tool.Name)
		}
		for index, step := range tool.Steps {
			if err := validateAction(step); err != nil {
				return fmt.Errorf("workflow tool %q step %d invalid: %w", tool.Name, index+1, err)
			}
		}
		for index, step := range tool.Finally {
			if err := validateAction(step); err != nil {
				return fmt.Errorf("workflow tool %q finally step %d invalid: %w", tool.Name, index+1, err)
			}
		}
		for actionName, actionSteps := range tool.Actions {
			if actionName == "" {
				return fmt.Errorf("workflow tool %q has empty action key", tool.Name)
			}
			if len(actionSteps) == 0 {
				return fmt.Errorf("workflow tool %q action %q has no steps", tool.Name, actionName)
			}
			for index, step := range actionSteps {
				if err := validateAction(step); err != nil {
					return fmt.Errorf("workflow tool %q action %q step %d invalid: %w", tool.Name, actionName, index+1, err)
				}
			}
		}
	default:
		return fmt.Errorf("unknown type %q", tool.Type)
	}
	switch tool.Result {
	case "", "text", "json":
	default:
		return fmt.Errorf("result must be one of: text, json")
	}
	if (tool.Extract != "" || len(tool.Select) > 0) && tool.Result != "json" {
		return fmt.Errorf("extract/select require result=json")
	}
	return nil
}

func validateAction(action ActionDefinition) error {
	switch action.Type {
	case "shell":
		if len(action.Command) == 0 {
			return fmt.Errorf("shell action has empty command")
		}
	case "http":
		if action.URL == "" {
			return fmt.Errorf("http action has empty url")
		}
	case "forEach":
		if action.ForEach == "" {
			return fmt.Errorf("forEach action requires forEach path")
		}
		if len(action.Steps) == 0 {
			return fmt.Errorf("forEach action requires nested steps")
		}
		for index, step := range action.Steps {
			if err := validateAction(step); err != nil {
				return fmt.Errorf("forEach nested step %d invalid: %w", index+1, err)
			}
		}
	case "switch":
		if action.Switch == "" {
			return fmt.Errorf("switch action requires switch expression")
		}
		if len(action.Cases) == 0 && len(action.Default) == 0 {
			return fmt.Errorf("switch action requires at least one case or default")
		}
		for caseIndex, switchCase := range action.Cases {
			if switchCase.Match == "" {
				return fmt.Errorf("switch case %d missing match", caseIndex+1)
			}
			if len(switchCase.Steps) == 0 {
				return fmt.Errorf("switch case %d has no steps", caseIndex+1)
			}
			for stepIndex, step := range switchCase.Steps {
				if err := validateAction(step); err != nil {
					return fmt.Errorf("switch case %d step %d invalid: %w", caseIndex+1, stepIndex+1, err)
				}
			}
		}
		for index, step := range action.Default {
			if err := validateAction(step); err != nil {
				return fmt.Errorf("switch default step %d invalid: %w", index+1, err)
			}
		}
	default:
		return fmt.Errorf("unknown action type %q", action.Type)
	}
	if action.Retries < 0 {
		return fmt.Errorf("retries must be >= 0")
	}
	switch action.OnError {
	case "", "fail", "continue":
	default:
		return fmt.Errorf("onError must be one of: fail, continue")
	}
	switch action.Result {
	case "", "text", "json":
	default:
		return fmt.Errorf("result must be one of: text, json")
	}
	if (action.Extract != "" || len(action.Select) > 0) && action.Result != "json" {
		return fmt.Errorf("extract/select require result=json")
	}
	if action.Auth != "" && action.Type != "http" {
		return fmt.Errorf("auth is only valid for http actions")
	}
	return nil
}

func validateSkillAuthReferences(skill SkillDefinition) error {
	for _, tool := range skill.Tools {
		if tool.Auth != "" {
			if _, ok := skill.HTTPAuth[tool.Auth]; !ok {
				return fmt.Errorf("tool %q references unknown auth profile %q", tool.Name, tool.Auth)
			}
		}
		for _, step := range tool.Steps {
			if err := validateActionAuthReferences(step, skill.HTTPAuth); err != nil {
				return fmt.Errorf("tool %q: %w", tool.Name, err)
			}
		}
		for _, step := range tool.Finally {
			if err := validateActionAuthReferences(step, skill.HTTPAuth); err != nil {
				return fmt.Errorf("tool %q finally: %w", tool.Name, err)
			}
		}
		for actionName, actionSteps := range tool.Actions {
			for _, step := range actionSteps {
				if err := validateActionAuthReferences(step, skill.HTTPAuth); err != nil {
					return fmt.Errorf("tool %q action %q: %w", tool.Name, actionName, err)
				}
			}
		}
	}
	return nil
}

func validateHTTPAuthProfiles(profiles map[string]HTTPAuthProfile) error {
	for name, profile := range profiles {
		if name == "" {
			return fmt.Errorf("httpAuth has empty profile name")
		}
		switch profile.Type {
		case "bearer":
			if profile.Token == "" {
				return fmt.Errorf("httpAuth profile %q missing token", name)
			}
		case "basic":
			if profile.Username == "" || profile.Password == "" {
				return fmt.Errorf("httpAuth profile %q requires username and password", name)
			}
		case "apiKey":
			if profile.Value == "" {
				return fmt.Errorf("httpAuth profile %q missing value", name)
			}
			if profile.Header == "" && profile.QueryParam == "" {
				return fmt.Errorf("httpAuth profile %q needs header or queryParam", name)
			}
		default:
			return fmt.Errorf("httpAuth profile %q has unsupported type %q", name, profile.Type)
		}
	}
	return nil
}

func validateActionAuthReferences(action ActionDefinition, profiles map[string]HTTPAuthProfile) error {
	if action.Auth != "" {
		if _, ok := profiles[action.Auth]; !ok {
			return fmt.Errorf("action %q references unknown auth profile %q", action.Name, action.Auth)
		}
	}
	for _, step := range action.Steps {
		if err := validateActionAuthReferences(step, profiles); err != nil {
			return err
		}
	}
	for _, switchCase := range action.Cases {
		for _, step := range switchCase.Steps {
			if err := validateActionAuthReferences(step, profiles); err != nil {
				return err
			}
		}
	}
	for _, step := range action.Default {
		if err := validateActionAuthReferences(step, profiles); err != nil {
			return err
		}
	}
	return nil
}

func isRuntimeCompatible(minVersion string) bool {
	minVersion = strings.TrimSpace(minVersion)
	if minVersion == "" {
		return true
	}
	current := strings.TrimSpace(version.Version())
	if current == "" || strings.EqualFold(current, "dev") {
		return true
	}
	return compareVersions(current, minVersion) >= 0
}
