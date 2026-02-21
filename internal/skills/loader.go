package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/version"
	"gopkg.in/yaml.v3"
)

// LoadAll reads all *.md files from skillsDirectory and returns parsed skills.
// Logs warnings for malformed files and continues.
func LoadAll(skillsDirectory string) ([]SkillDefinition, error) {
	entries, err := os.ReadDir(skillsDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var (
		skills      []SkillDefinition
		localNames  = map[string]bool{}
		installedBy = map[string]installedSkill{}
	)

	loadFile := func(path string, sourceName string) (SkillDefinition, bool) {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Warningf("failed to read %s: %v", sourceName, err)
			return SkillDefinition{}, false
		}

		var skill SkillDefinition
		body, err := parseSkillMarkdown(data, &skill)
		if err != nil {
			log.Warningf("failed to parse %s: %v", sourceName, err)
			return SkillDefinition{}, false
		}
		// Prompt text comes from markdown body.
		skill.Prompt = strings.TrimSpace(body)

		if skill.Name == "" {
			log.Warningf("%s missing name, skipping", sourceName)
			return SkillDefinition{}, false
		}
		if !isRuntimeCompatible(skill.RuntimeMinVersion) {
			log.Warningf("skill %s requires runtime >= %s, skipping", skill.Name, skill.RuntimeMinVersion)
			return SkillDefinition{}, false
		}

		// Validate tools, keeping only valid ones.
		var validTools []ToolDefinition
		for _, tool := range skill.Tools {
			if err := validateTool(tool); err != nil {
				log.Warningf("skill %s: tool %q invalid: %v", skill.Name, tool.Name, err)
				continue
			}
			validTools = append(validTools, tool)
		}
		skill.Tools = validTools

		if len(skill.Tools) == 0 {
			log.Warningf("skill %s: no valid tools, skipping", skill.Name)
			return SkillDefinition{}, false
		}
		if err := validateHTTPAuthProfiles(skill.HTTPAuth); err != nil {
			log.Warningf("skill %s: %v", skill.Name, err)
			return SkillDefinition{}, false
		}
		if err := validateSkillAuthReferences(skill); err != nil {
			log.Warningf("skill %s: %v", skill.Name, err)
			return SkillDefinition{}, false
		}
		return skill, true
	}

	// Load local top-level skills first.
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(skillsDirectory, entry.Name())
		skill, ok := loadFile(path, entry.Name())
		if !ok {
			continue
		}
		skill.IsLocal = true
		localNames[skill.Name] = true
		skills = append(skills, skill)
	}

	// Load installed registry skills from .installed/<name>/<version>/skill.md.
	installedRoot := filepath.Join(skillsDirectory, ".installed")
	_ = filepath.WalkDir(installedRoot, func(path string, directoryEntry os.DirEntry, err error) error {
		if err != nil || directoryEntry == nil || directoryEntry.IsDir() {
			return nil
		}
		if filepath.Base(path) != "skill.md" {
			return nil
		}
		enabled, manifestErr := loadInstalledSkillEnabled(filepath.Dir(path))
		if manifestErr != nil {
			log.Warningf("failed to read installed skill manifest for %s: %v", path, manifestErr)
		}
		if !enabled {
			return nil
		}
		skill, ok := loadFile(path, path)
		if !ok {
			return nil
		}
		skill.IsLocal = false
		if localNames[skill.Name] {
			return nil
		}

		version := filepath.Base(filepath.Dir(path))
		existing, exists := installedBy[skill.Name]
		if !exists || compareVersions(version, existing.Version) > 0 {
			installedBy[skill.Name] = installedSkill{Definition: skill, Version: version}
		}
		return nil
	})

	for _, installed := range installedBy {
		skills = append(skills, installed.Definition)
	}

	return skills, nil
}

func loadInstalledSkillEnabled(versionDirectory string) (bool, error) {
	manifestPath := filepath.Join(versionDirectory, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return true, err
	}

	var manifest installManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return true, err
	}
	if manifest.Enabled == nil {
		return true, nil
	}
	return *manifest.Enabled, nil
}

type installedSkill struct {
	Definition SkillDefinition
	Version    string
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
