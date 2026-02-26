package fsstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/ptrto"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

type fileSystemSkillFrontmatter struct {
	Name                   string                                        `yaml:"name"`
	Description            string                                        `yaml:"description,omitempty"`
	Version                string                                        `yaml:"version,omitempty"`
	AuthenticationProfiles map[string]models.SkillAuthenticationProfiles `yaml:"authenticationProfiles,omitempty"`
	Tools                  []*models.SkillTool                           `yaml:"tools,omitempty"`
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
	Name                   string
	Description            string
	Version                string
	AuthenticationProfiles map[string]models.SkillAuthenticationProfiles
	Tools                  []*models.SkillTool
	Prompt                 string
	Source                 string
	Publisher              string
	InstalledAt            string
	Enabled                bool
}

func buildSkillFrontmatter(skill models.Skill, skillId string) map[string]interface{} {
	frontmatter := map[string]interface{}{}
	name := skill.GetName()
	if name == "" {
		name = skillId
	}
	frontmatter["name"] = name
	version := skill.GetVersion()
	if version != "" {
		frontmatter["version"] = version
	}
	description := skill.GetDescription()
	if description != "" {
		frontmatter["description"] = description
	}
	if skill.AuthenticationProfiles != nil {
		frontmatter["authenticationProfiles"] = *skill.AuthenticationProfiles
	}
	if skill.Tools != nil {
		frontmatter["tools"] = *skill.Tools
	}
	return frontmatter
}

func (self *fileSystemTransaction) ListSkills(ctx context.Context, options *store.Option) ([]*models.Skill, error) {
	return self.listSkills(options)
}

func (self *fileSystemTransaction) CreateSkill(ctx context.Context, skill *models.Skill, options *store.Option) (*models.Skill, error) {
	return self.createSkill(skill, options)
}

func (self *fileSystemTransaction) GetSkill(ctx context.Context, skillId string, options *store.Option) (*models.Skill, error) {
	return self.getSkill(ctx, skillId, options)
}

func (self *fileSystemTransaction) ModifySkill(ctx context.Context, skillId string, modifier func(*models.Skill) error, options *store.Option) (*models.Skill, error) {
	return self.modifySkill(ctx, skillId, modifier, options)
}

func (self *fileSystemTransaction) DeleteSkill(ctx context.Context, skillId string, options *store.Option) error {
	return self.deleteSkill(skillId, options)
}

func (self *fileSystemTransaction) listSkills(options *store.Option) ([]*models.Skill, error) {
	parsedSkills, loadError := self.loadSkillsFromFileSystem()
	if loadError != nil {
		return nil, loadError
	}
	results := make([]*models.Skill, 0, len(parsedSkills))
	for _, parsedSkill := range parsedSkills {
		name := parsedSkill.Name
		prompt := parsedSkill.Prompt
		description := parsedSkill.Description
		skill := models.Skill{
			ID:          parsedSkill.Name,
			Name:        ptrto.TrimmedString(name),
			Description: ptrto.TrimmedString(description),
			Version:     ptrto.TrimmedString(parsedSkill.Version),
			Source:      ptrto.TrimmedString(parsedSkill.Source),
			Publisher:   ptrto.TrimmedString(parsedSkill.Publisher),
			Prompt:      ptrto.Value(prompt),
		}
		if len(parsedSkill.AuthenticationProfiles) > 0 {
			authenticationProfiles := parsedSkill.AuthenticationProfiles
			skill.AuthenticationProfiles = &authenticationProfiles
		}
		if len(parsedSkill.Tools) > 0 {
			tools := parsedSkill.Tools
			skill.Tools = &tools
		}
		enabledValue := parsedSkill.Enabled
		skill.Enabled = &enabledValue
		results = append(results, &skill)
	}
	return applyOffsetLimit(results, options), nil
}

func (self *fileSystemTransaction) createSkill(skill *models.Skill, options *store.Option) (*models.Skill, error) {
	if skill == nil {
		return nil, store.ErrInvalidOptions
	}
	skillId := skill.ID
	if skillId == "" {
		skillId = skill.GetName()
	}
	if skillId == "" {
		skillId = security.NewULID()
	}
	createdSkill := *skill
	createdSkill.ID = skillId
	if createdSkill.Name == nil {
		createdSkill.Name = ptrto.Value(skillId)
	}
	if createdSkill.Version == nil || createdSkill.GetVersion() == "" {
		createdSkill.Version = ptrto.Value("0.0.0")
	}
	now := ptrto.TimeNowInLocal()
	createdSkill.CreatedAt = now
	createdSkill.ModifiedAt = now
	if writeError := self.writeInstalledSkillFiles(skillId, createdSkill); writeError != nil {
		return nil, writeError
	}
	return &createdSkill, nil
}

func (self *fileSystemTransaction) getSkill(ctx context.Context, skillId string, options *store.Option) (*models.Skill, error) {
	// Look up the skill directory directly instead of listing all skills.
	skillDirectory := filepath.Join(self.skillsDirectory(), skillId)
	versionEntries, readError := os.ReadDir(skillDirectory)
	if readError != nil {
		return nil, store.ErrNotFound
	}

	// Find the highest version that contains a valid skill.md.
	var bestParsedSkill *fileSystemParsedSkill
	bestVersion := ""
	for _, versionEntry := range versionEntries {
		if !versionEntry.IsDir() {
			continue
		}
		versionDirectory := filepath.Join(skillDirectory, versionEntry.Name())
		skillPath := filepath.Join(versionDirectory, "skill.md")
		parsedSkill, parseError := self.readSkillMarkdown(skillPath)
		if parseError != nil {
			continue
		}
		manifestPath := filepath.Join(versionDirectory, "manifest.json")
		manifest := fileSystemInstalledSkillManifest{}
		enabled := true
		if data, readManifestError := os.ReadFile(manifestPath); readManifestError == nil {
			if unmarshalError := json.Unmarshal(data, &manifest); unmarshalError == nil {
				if manifest.Enabled != nil {
					enabled = *manifest.Enabled
				}
			}
		}
		parsedSkill.Enabled = enabled
		parsedSkill.Source = manifest.SourceID
		parsedSkill.Publisher = manifest.Publisher
		parsedSkill.InstalledAt = manifest.InstalledAt
		if parsedSkill.Name == "" {
			parsedSkill.Name = skillId
		}
		if parsedSkill.Version == "" {
			parsedSkill.Version = manifest.Version
		}
		installedVersion := versionEntry.Name()
		if bestParsedSkill == nil || compareSkillVersions(installedVersion, bestVersion) > 0 {
			bestParsedSkill = &parsedSkill
			bestVersion = installedVersion
		}
	}
	if bestParsedSkill == nil {
		return nil, store.ErrNotFound
	}

	skill := &models.Skill{
		ID:          bestParsedSkill.Name,
		Name:        ptrto.TrimmedString(bestParsedSkill.Name),
		Description: ptrto.TrimmedString(bestParsedSkill.Description),
		Version:     ptrto.TrimmedString(bestParsedSkill.Version),
		Source:      ptrto.TrimmedString(bestParsedSkill.Source),
		Publisher:   ptrto.TrimmedString(bestParsedSkill.Publisher),
		Prompt:      ptrto.Value(bestParsedSkill.Prompt),
	}
	if len(bestParsedSkill.AuthenticationProfiles) > 0 {
		authenticationProfiles := bestParsedSkill.AuthenticationProfiles
		skill.AuthenticationProfiles = &authenticationProfiles
	}
	if len(bestParsedSkill.Tools) > 0 {
		tools := bestParsedSkill.Tools
		skill.Tools = &tools
	}
	enabledValue := bestParsedSkill.Enabled
	skill.Enabled = &enabledValue
	return skill, nil
}

func (self *fileSystemTransaction) modifySkill(ctx context.Context, skillId string, modifier func(*models.Skill) error, options *store.Option) (*models.Skill, error) {
	skill, getError := self.GetSkill(ctx, skillId, options)
	if getError != nil {
		return nil, getError
	}
	if modifierError := modifier(skill); modifierError != nil {
		return nil, modifierError
	}
	if skill.ID == "" {
		skill.ID = skillId
	}
	if skill.Version == nil || skill.GetVersion() == "" {
		skill.Version = ptrto.Value("0.0.0")
	}
	skill.ModifiedAt = ptrto.TimeNowInLocal()
	if writeError := self.writeInstalledSkillFiles(skillId, *skill); writeError != nil {
		return nil, writeError
	}
	return skill, nil
}

func (self *fileSystemTransaction) deleteSkill(skillId string, options *store.Option) error {
	installedSkillDirectory := filepath.Join(self.skillsDirectory(), skillId)
	if _, statError := os.Stat(installedSkillDirectory); os.IsNotExist(statError) {
		return store.ErrNotFound
	}
	return trash.Move(installedSkillDirectory, self.trashDirectory())
}

func (self *fileSystemTransaction) writeInstalledSkillFiles(skillId string, skill models.Skill) error {
	version := skill.GetVersion()
	if version == "" {
		version = "0.0.0"
	}
	versionDirectory := filepath.Join(self.skillsDirectory(), skillId, version)
	if makeDirectoryError := os.MkdirAll(versionDirectory, 0755); makeDirectoryError != nil {
		return makeDirectoryError
	}
	frontmatter := buildSkillFrontmatter(skill, skillId)
	yamlData, marshalError := yaml.Marshal(frontmatter)
	if marshalError != nil {
		return marshalError
	}
	prompt := skill.GetPrompt()
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
	if skill.Enabled != nil {
		enabled = *skill.Enabled
	}
	manifest := fileSystemInstalledSkillManifest{
		Name:        skill.GetName(),
		Description: skill.GetDescription(),
		Version:     version,
		Enabled:     ptrto.Value(enabled),
		SourceID:    skill.GetSource(),
		Publisher:   skill.GetPublisher(),
	}
	manifestData, marshalError := json.MarshalIndent(manifest, "", "  ")
	if marshalError != nil {
		return marshalError
	}
	return atomicfile.WriteFile(filepath.Join(versionDirectory, "manifest.json"), manifestData)
}

func (self *fileSystemTransaction) loadSkillsFromFileSystem() ([]fileSystemParsedSkill, error) {
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

	installedRoot := self.skillsDirectory()
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
		skill.Source = manifest.SourceID
		skill.Publisher = manifest.Publisher
		skill.InstalledAt = manifest.InstalledAt
		if skill.Name == "" {
			skill.Name = filepath.Base(filepath.Dir(filepath.Dir(path)))
		}
		if skill.Version == "" {
			skill.Version = manifest.Version
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

func (self *fileSystemTransaction) readSkillMarkdown(path string) (fileSystemParsedSkill, error) {
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
		Name:                   frontmatter.Name,
		Description:            frontmatter.Description,
		Version:                frontmatter.Version,
		AuthenticationProfiles: frontmatter.AuthenticationProfiles,
		Tools:                  frontmatter.Tools,
		Prompt:                 strings.TrimSpace(body),
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
