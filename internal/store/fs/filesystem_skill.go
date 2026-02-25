package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

type fileSystemSkillFrontmatter struct {
	Name              string                   `yaml:"name"`
	Description       string                   `yaml:"description,omitempty"`
	Version           string                   `yaml:"version,omitempty"`
	RuntimeMinVersion string                   `yaml:"runtimeMinVersion,omitempty"`
	HTTPAuth          map[string]interface{}   `yaml:"httpAuth,omitempty"`
	Tools             []map[string]interface{} `yaml:"tools,omitempty"`
}

type fileSystemInstalledSkillManifest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	SourceID    string `json:"sourceId,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	InstalledAt string `json:"installedAt,omitempty"`
}

type fileSystemParsedSkill struct {
	Name              string
	Description       string
	Version           string
	RuntimeMinVersion string
	HTTPAuth          map[string]interface{}
	Tools             []map[string]interface{}
	Prompt            string
	Source            string
	Publisher         string
	InstalledAt       string
	Enabled           bool
}

func buildSkillFrontmatter(skill models.Skill, skillId string) map[string]interface{} {
	frontmatter := map[string]interface{}{}
	name := strings.TrimSpace(valueOrEmpty(skill.Name))
	if name == "" {
		name = skillId
	}
	frontmatter["name"] = name
	version := strings.TrimSpace(valueOrEmpty(skill.Version))
	if version != "" {
		frontmatter["version"] = version
	}
	description := strings.TrimSpace(descriptionFromSkillMetadata(skill.Metadata))
	if description == "" {
		description = strings.TrimSpace(valueOrEmpty(skill.Description))
	}
	if description != "" {
		frontmatter["description"] = description
	}
	runtimeMinVersion := strings.TrimSpace(valueOrEmpty(skill.RuntimeMinVersion))
	if runtimeMinVersion != "" {
		frontmatter["runtimeMinVersion"] = runtimeMinVersion
	}
	if skill.HTTPAuth != nil {
		frontmatter["httpAuth"] = *skill.HTTPAuth
	}
	if skill.Tools != nil {
		frontmatter["tools"] = *skill.Tools
	}
	return frontmatter
}

func (self *transaction) ListSkills(options *store.Option) ([]models.Skill, error) {
	return self.listSkills(options)
}

func (self *transaction) CreateSkill(skill *models.Skill, options *store.Option) (*models.Skill, error) {
	return self.createSkill(skill, options)
}

func (self *transaction) GetSkill(skillId string, options *store.Option) (*models.Skill, error) {
	return self.getSkill(skillId, options)
}

func (self *transaction) ModifySkill(skillId string, modifier func(*models.Skill) error, options *store.Option) (*models.Skill, error) {
	return self.modifySkill(skillId, modifier, options)
}

func (self *transaction) DeleteSkill(skillId string, options *store.Option) error {
	return self.deleteSkill(skillId, options)
}

func (self *transaction) listSkills(options *store.Option) ([]models.Skill, error) {
	parsedSkills, loadError := self.loadSkillsFromFileSystem()
	if loadError != nil {
		return nil, loadError
	}
	results := make([]models.Skill, 0, len(parsedSkills))
	for _, parsedSkill := range parsedSkills {
		name := parsedSkill.Name
		prompt := parsedSkill.Prompt
		description := parsedSkill.Description
		metadata := map[string]interface{}{
			"description": description,
			"enabled":     parsedSkill.Enabled,
			"isLocal":     false,
		}
		if parsedSkill.Source != "" {
			metadata["source"] = parsedSkill.Source
		}
		if parsedSkill.Publisher != "" {
			metadata["publisher"] = parsedSkill.Publisher
		}
		if parsedSkill.InstalledAt != "" {
			metadata["installedAt"] = parsedSkill.InstalledAt
		}
		skill := models.Skill{
			ID:                strings.TrimSpace(parsedSkill.Name),
			Name:              ptrto.TrimmedString(name),
			Description:       ptrto.TrimmedString(description),
			Version:           ptrto.TrimmedString(parsedSkill.Version),
			RuntimeMinVersion: ptrto.TrimmedString(parsedSkill.RuntimeMinVersion),
			Source:            ptrto.TrimmedString(parsedSkill.Source),
			Publisher:         ptrto.TrimmedString(parsedSkill.Publisher),
			Prompt:            ptrto.Value(prompt),
			Metadata:          &metadata,
		}
		if len(parsedSkill.HTTPAuth) > 0 {
			httpAuth := parsedSkill.HTTPAuth
			skill.HTTPAuth = &httpAuth
		}
		if len(parsedSkill.Tools) > 0 {
			tools := parsedSkill.Tools
			skill.Tools = &tools
		}
		enabledValue := parsedSkill.Enabled
		skill.Enabled = &enabledValue
		results = append(results, skill)
	}
	return applyOffsetLimitSkills(results, options), nil
}

func (self *transaction) createSkill(skill *models.Skill, options *store.Option) (*models.Skill, error) {
	if skill == nil {
		return nil, store.ErrInvalidOptions
	}
	skillId := strings.TrimSpace(skill.ID)
	if skillId == "" {
		skillId = strings.TrimSpace(valueOrEmpty(skill.Name))
	}
	if skillId == "" {
		skillId = security.NewULID()
	}
	createdSkill := *skill
	createdSkill.ID = skillId
	if createdSkill.Name == nil {
		createdSkill.Name = ptrto.Value(skillId)
	}
	if createdSkill.Version == nil || strings.TrimSpace(valueOrEmpty(createdSkill.Version)) == "" {
		createdSkill.Version = ptrto.Value("0.0.0")
	}
	now := time.Now()
	if createdSkill.CreatedAt == nil {
		createdSkill.CreatedAt = &now
	}
	createdSkill.ModifiedAt = &now
	if writeError := self.writeInstalledSkillFiles(skillId, createdSkill); writeError != nil {
		return nil, writeError
	}
	return &createdSkill, nil
}

func (self *transaction) getSkill(skillId string, options *store.Option) (*models.Skill, error) {
	skillsList, listError := self.ListSkills(nil)
	if listError != nil {
		return nil, listError
	}
	for _, listedSkill := range skillsList {
		if listedSkill.ID == skillId {
			return &listedSkill, nil
		}
	}
	return nil, store.ErrNotFound
}

func (self *transaction) modifySkill(skillId string, modifier func(*models.Skill) error, options *store.Option) (*models.Skill, error) {
	skill, getError := self.GetSkill(skillId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(skill); modifierError != nil {
		return nil, modifierError
	}
	if strings.TrimSpace(skill.ID) == "" {
		skill.ID = skillId
	}
	if skill.Version == nil || strings.TrimSpace(valueOrEmpty(skill.Version)) == "" {
		skill.Version = ptrto.Value("0.0.0")
	}
	now := time.Now()
	skill.ModifiedAt = &now
	if writeError := self.writeInstalledSkillFiles(skillId, *skill); writeError != nil {
		return nil, writeError
	}
	return skill, nil
}

func (self *transaction) deleteSkill(skillId string, options *store.Option) error {
	installedSkillDirectory := filepath.Join(self.skillsDirectory(), ".installed", skillId)
	if _, statError := os.Stat(installedSkillDirectory); os.IsNotExist(statError) {
		return nil
	}
	return trash.Move(installedSkillDirectory, self.trashDirectory())
}

func (self *transaction) writeInstalledSkillFiles(skillId string, skill models.Skill) error {
	version := strings.TrimSpace(valueOrEmpty(skill.Version))
	if version == "" {
		version = "0.0.0"
	}
	versionDirectory := filepath.Join(self.skillsDirectory(), ".installed", skillId, version)
	if makeDirectoryError := os.MkdirAll(versionDirectory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	frontmatter := buildSkillFrontmatter(skill, skillId)
	yamlData, marshalError := yaml.Marshal(frontmatter)
	if marshalError != nil {
		return marshalError
	}
	prompt := strings.TrimSpace(valueOrEmpty(skill.Prompt))
	content := strings.Builder{}
	content.WriteString("---\n")
	content.Write(yamlData)
	content.WriteString("---\n\n")
	content.WriteString(prompt)
	content.WriteString("\n")
	if writeError := atomicfile.WriteFile(filepath.Join(versionDirectory, "skill.md"), []byte(content.String())); writeError != nil {
		return writeError
	}
	enabled := true
	if skill.Metadata != nil {
		if enabledValue, exists := (*skill.Metadata)["enabled"]; exists {
			if enabledBool, ok := enabledValue.(bool); ok {
				enabled = enabledBool
			}
		}
	}
	manifest := fileSystemInstalledSkillManifest{
		Name: strings.TrimSpace(valueOrEmpty(skill.Name)),
		Description: firstNonEmpty(
			strings.TrimSpace(valueOrEmpty(skill.Description)),
			strings.TrimSpace(descriptionFromSkillMetadata(skill.Metadata)),
		),
		Version:  version,
		Enabled:  ptrto.Value(enabled),
		SourceID: strings.TrimSpace(valueOrEmpty(skill.Source)),
		Publisher: firstNonEmpty(
			strings.TrimSpace(valueOrEmpty(skill.Publisher)),
			metadataStringFromMap(skill.Metadata, "publisher"),
		),
		InstalledAt: metadataStringFromMap(skill.Metadata, "installedAt"),
	}
	manifestData, marshalError := json.MarshalIndent(manifest, "", "  ")
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFile(filepath.Join(versionDirectory, "manifest.json"), manifestData)
}

func (self *transaction) loadSkillsFromFileSystem() ([]fileSystemParsedSkill, error) {
	_, readError := os.ReadDir(self.skillsDirectory())
	if os.IsNotExist(readError) {
		return []fileSystemParsedSkill{}, nil
	}
	if readError != nil {
		return nil, readError
	}

	results := make([]fileSystemParsedSkill, 0)
	installedSkillsByName := map[string]struct {
		skill   fileSystemParsedSkill
		version string
	}{}

	installedRoot := filepath.Join(self.skillsDirectory(), ".installed")
	_ = filepath.WalkDir(installedRoot, func(path string, directoryEntry os.DirEntry, walkError error) error {
		if walkError != nil || directoryEntry == nil || directoryEntry.IsDir() {
			return nil
		}
		if filepath.Base(path) != "skill.md" {
			return nil
		}
		manifestPath := filepath.Join(filepath.Dir(path), "manifest.json")
		manifest := fileSystemInstalledSkillManifest{}
		enabled := true
		if data, readManifestError := os.ReadFile(manifestPath); readManifestError == nil {
			if unmarshalError := json.Unmarshal(data, &manifest); unmarshalError == nil {
				if manifest.Enabled != nil {
					enabled = *manifest.Enabled
				}
			}
		}
		skill, parseError := self.readSkillMarkdown(path)
		if parseError != nil {
			return nil
		}
		skill.Enabled = enabled
		skill.Source = strings.TrimSpace(manifest.SourceID)
		skill.Publisher = strings.TrimSpace(manifest.Publisher)
		skill.InstalledAt = strings.TrimSpace(manifest.InstalledAt)
		if strings.TrimSpace(skill.Name) == "" {
			skill.Name = filepath.Base(filepath.Dir(filepath.Dir(path)))
		}
		if strings.TrimSpace(skill.Version) == "" {
			skill.Version = strings.TrimSpace(manifest.Version)
		}
		installedVersion := filepath.Base(filepath.Dir(path))
		existing, exists := installedSkillsByName[skill.Name]
		if !exists || compareSkillVersions(installedVersion, existing.version) > 0 {
			installedSkillsByName[skill.Name] = struct {
				skill   fileSystemParsedSkill
				version string
			}{skill: skill, version: installedVersion}
		}
		return nil
	})

	for _, installedEntry := range installedSkillsByName {
		results = append(results, installedEntry.skill)
	}

	sort.Slice(results, func(leftIndex int, rightIndex int) bool {
		return results[leftIndex].Name < results[rightIndex].Name
	})

	return results, nil
}

func (self *transaction) readSkillMarkdown(path string) (fileSystemParsedSkill, error) {
	data, readError := os.ReadFile(path)
	if readError != nil {
		return fileSystemParsedSkill{}, readError
	}
	frontmatter := fileSystemSkillFrontmatter{}
	body, parseError := parseSkillMarkdownFrontmatter(data, &frontmatter)
	if parseError != nil {
		return fileSystemParsedSkill{}, parseError
	}
	return fileSystemParsedSkill{
		Name:              strings.TrimSpace(frontmatter.Name),
		Description:       strings.TrimSpace(frontmatter.Description),
		Version:           strings.TrimSpace(frontmatter.Version),
		RuntimeMinVersion: strings.TrimSpace(frontmatter.RuntimeMinVersion),
		HTTPAuth:          frontmatter.HTTPAuth,
		Tools:             frontmatter.Tools,
		Prompt:            strings.TrimSpace(body),
	}, nil
}

func parseSkillMarkdownFrontmatter(data []byte, frontmatter *fileSystemSkillFrontmatter) (string, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", fmt.Errorf("missing frontmatter delimiter")
	}
	rest := content[4:]
	closingIndex := strings.Index(rest, "\n---\n")
	if closingIndex < 0 {
		if strings.HasSuffix(rest, "\n---") {
			closingIndex = len(rest) - 4
		} else {
			return "", fmt.Errorf("missing closing frontmatter delimiter")
		}
	}
	frontmatterYAML := rest[:closingIndex]
	if unmarshalError := yaml.Unmarshal([]byte(frontmatterYAML), frontmatter); unmarshalError != nil {
		return "", fmt.Errorf("parsing frontmatter: %w", unmarshalError)
	}
	bodyStart := closingIndex + 5
	if bodyStart > len(rest) {
		return "", nil
	}
	return rest[bodyStart:], nil
}

func descriptionFromSkillMetadata(metadata *map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	descriptionValue, exists := (*metadata)["description"]
	if !exists {
		return ""
	}
	switch typedValue := descriptionValue.(type) {
	case string:
		return typedValue
	default:
		return ""
	}
}

func metadataStringFromMap(metadata *map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, exists := (*metadata)[key]
	if !exists {
		return ""
	}
	stringValue, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue != "" {
			return trimmedValue
		}
	}
	return ""
}

func compareSkillVersions(leftVersion string, rightVersion string) int {
	leftParts := strings.Split(strings.TrimPrefix(leftVersion, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(rightVersion, "v"), ".")
	maxLength := len(leftParts)
	if len(rightParts) > maxLength {
		maxLength = len(rightParts)
	}
	for index := 0; index < maxLength; index++ {
		leftPart := "0"
		rightPart := "0"
		if index < len(leftParts) {
			leftPart = leftParts[index]
		}
		if index < len(rightParts) {
			rightPart = rightParts[index]
		}
		leftNumber, leftError := strconv.Atoi(leftPart)
		rightNumber, rightError := strconv.Atoi(rightPart)
		if leftError == nil && rightError == nil {
			if leftNumber > rightNumber {
				return 1
			}
			if leftNumber < rightNumber {
				return -1
			}
			continue
		}
		if leftPart > rightPart {
			return 1
		}
		if leftPart < rightPart {
			return -1
		}
	}
	return 0
}

func applyOffsetLimitSkills(values []models.Skill, options *store.Option) []models.Skill {
	if options == nil {
		return values
	}
	offset := int(uint64Value(options.Offset))
	if offset >= len(values) {
		return []models.Skill{}
	}
	values = values[offset:]
	limit := int(uint64Value(options.Limit))
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}
