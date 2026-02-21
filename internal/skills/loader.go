package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
		skill, ok := loadFile(path, path)
		if !ok {
			return nil
		}
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

type installedSkill struct {
	Definition SkillDefinition
	Version    string
}

func parseSkillMarkdown(data []byte, definition *SkillDefinition) (string, error) {
	content := string(data)

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
	default:
		return fmt.Errorf("unknown type %q", tool.Type)
	}
	return nil
}
