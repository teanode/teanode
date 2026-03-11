package skills

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/ptrto"
	"gopkg.in/yaml.v3"
)

const (
	officialIndexUrl  = "https://raw.githubusercontent.com/teanode/teanode-skills/main/index.json"
	officialPublicKey = "lPFKUpWbq3G1EykDv6SvsAACW0W/FZUaPiRyFlmEfj4="
)

// Index is the remote skill registry manifest.
type Index struct {
	Publisher string       `json:"publisher"`
	Skills    []SkillEntry `json:"skills"`
}

// SkillEntry is one skill in the remote index.
type SkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	URL         string   `json:"url"`
	SHA256      string   `json:"sha256"`
	Signature   string   `json:"signature"`
	Tags        []string `json:"tags"`
}

// SearchResult is a skill returned from a library search.
type SearchResult struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
}

// InstalledSkillInfo is the response from Install/Update.
type InstalledSkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
	Publisher   string `json:"publisher"`
}

// FetchIndex downloads and parses the official skill index.
func FetchIndex(ctx context.Context) (*Index, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, officialIndexUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("creating index request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching index: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("index returned status %d", response.StatusCode)
	}
	var index Index
	if err := json.NewDecoder(response.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("parsing index: %w", err)
	}
	return &index, nil
}

// verifyEntrySignature verifies the Ed25519 signature for a skill entry.
func verifyEntrySignature(entry SkillEntry) error {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(officialPublicKey)
	if err != nil {
		return fmt.Errorf("decoding public key: %w", err)
	}
	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid public key length: %d", len(publicKeyBytes))
	}
	publicKey := ed25519.PublicKey(publicKeyBytes)

	signatureBytes, err := base64.StdEncoding.DecodeString(entry.Signature)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}

	// Message format matches sign-index.sh: "name\nversion\nurl\nlowercase(sha256)"
	message := entry.Name + "\n" + entry.Version + "\n" + entry.URL + "\n" + strings.ToLower(entry.SHA256)

	if !ed25519.Verify(publicKey, []byte(message), signatureBytes) {
		return fmt.Errorf("signature verification failed for %s", entry.Name)
	}
	return nil
}

// Search fetches the index and filters entries matching the query.
func Search(ctx context.Context, query string) ([]SearchResult, error) {
	index, err := FetchIndex(ctx)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	var results []SearchResult
	for _, entry := range index.Skills {
		if matchesQuery(entry, query) {
			results = append(results, SearchResult{
				Name:        entry.Name,
				Description: entry.Description,
				Version:     entry.Version,
				Tags:        entry.Tags,
			})
		}
	}
	return results, nil
}

// Install downloads, verifies, and installs a skill from the library.
func Install(ctx context.Context, name, version string) (*InstalledSkillInfo, error) {
	index, err := FetchIndex(ctx)
	if err != nil {
		return nil, err
	}

	// Find matching entry.
	var entry *SkillEntry
	for entryIndex := range index.Skills {
		if index.Skills[entryIndex].Name == name {
			if version == "" || index.Skills[entryIndex].Version == version {
				entry = &index.Skills[entryIndex]
				break
			}
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("skill %q not found in library", name)
	}

	// Verify signature.
	if err := verifyEntrySignature(*entry); err != nil {
		return nil, err
	}

	// Download skill content.
	body, err := downloadSkill(ctx, entry.URL)
	if err != nil {
		return nil, err
	}

	// Verify SHA256.
	hash := sha256.Sum256(body)
	actualHash := hex.EncodeToString(hash[:])
	if !strings.EqualFold(actualHash, entry.SHA256) {
		return nil, fmt.Errorf("sha256 mismatch for %s: expected %s, got %s", name, entry.SHA256, actualHash)
	}

	// Parse frontmatter to extract tools and prompt.
	frontmatter, prompt, err := parseFrontmatter(body)
	if err != nil {
		return nil, fmt.Errorf("parsing skill %s: %w", name, err)
	}

	// Build skill model.
	enabled := true
	skill := &models.Skill{
		ID:          name,
		Name:        ptrto.Value(name),
		Description: ptrto.Value(entry.Description),
		Version:     ptrto.Value(entry.Version),
		Source:      ptrto.Value(entry.URL),
		Publisher:   ptrto.Value(index.Publisher),
		Prompt:      ptrto.Value(prompt),
		Enabled:     &enabled,
	}
	if len(frontmatter.Tools) > 0 {
		tools := frontmatter.Tools
		skill.Tools = &tools
	}
	if len(frontmatter.AuthenticationProfiles) > 0 {
		authenticationProfiles := frontmatter.AuthenticationProfiles
		skill.AuthenticationProfiles = &authenticationProfiles
	}
	if len(frontmatter.Secrets) > 0 {
		secrets := frontmatter.Secrets
		skill.Secrets = &secrets
	}

	// Upsert in store.
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		existing, err := tx.GetSkill(ctx, name, nil)
		if err != nil {
			// Not found, create.
			_, err = tx.CreateSkill(ctx, skill, nil)
			return err
		}
		// Exists, update.
		_, err = tx.ModifySkill(ctx, existing.ID, func(existingSkill *models.Skill) error {
			existingSkill.Name = skill.Name
			existingSkill.Description = skill.Description
			existingSkill.Version = skill.Version
			existingSkill.Source = skill.Source
			existingSkill.Publisher = skill.Publisher
			existingSkill.Prompt = skill.Prompt
			existingSkill.Tools = skill.Tools
			existingSkill.AuthenticationProfiles = skill.AuthenticationProfiles
			existingSkill.Secrets = skill.Secrets
			// Preserve enabled state on reinstall.
			return nil
		}, nil)
		return err
	}); err != nil {
		return nil, fmt.Errorf("storing skill %s: %w", name, err)
	}

	return &InstalledSkillInfo{
		Name:        name,
		Description: entry.Description,
		Version:     entry.Version,
		Enabled:     enabled,
		Publisher:   index.Publisher,
	}, nil
}

// Update checks for newer versions and reinstalls if available.
// If name is empty, all installed skills are checked.
func Update(ctx context.Context, name string) ([]InstalledSkillInfo, error) {
	index, err := FetchIndex(ctx)
	if err != nil {
		return nil, err
	}

	// Build index lookup.
	indexMap := make(map[string]SkillEntry, len(index.Skills))
	for _, entry := range index.Skills {
		indexMap[entry.Name] = entry
	}

	// Load installed skills.
	var installed []*models.Skill
	if err := store.StoreFromContext(ctx).Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		installed, err = tx.ListSkills(ctx, nil)
		return err
	}); err != nil {
		return nil, fmt.Errorf("listing installed skills: %w", err)
	}

	var updated []InstalledSkillInfo
	for _, skill := range installed {
		skillName := skill.GetName()
		if name != "" && skillName != name {
			continue
		}
		entry, found := indexMap[skillName]
		if !found {
			continue
		}
		if compareVersions(skill.GetVersion(), entry.Version) >= 0 {
			continue
		}
		info, err := Install(ctx, skillName, entry.Version)
		if err != nil {
			log.Warningf("failed to update skill %s: %v", skillName, err)
			continue
		}
		updated = append(updated, *info)
	}
	return updated, nil
}

// compareVersions compares two semver-ish version strings.
// Returns -1 if left < right, 0 if equal, 1 if left > right.
func compareVersions(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	maxLen := len(leftParts)
	if len(rightParts) > maxLen {
		maxLen = len(rightParts)
	}
	for index := 0; index < maxLen; index++ {
		leftValue, rightValue := 0, 0
		if index < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[index])
		}
		if index < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[index])
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func downloadSkill(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("downloading skill: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("reading skill body: %w", err)
	}
	return body, nil
}

func matchesQuery(entry SkillEntry, query string) bool {
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Description), query) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

// libraryFrontmatter mirrors the YAML frontmatter structure.
type libraryFrontmatter struct {
	Name                   string                                        `yaml:"name"`
	Description            string                                        `yaml:"description,omitempty"`
	Version                string                                        `yaml:"version,omitempty"`
	AuthenticationProfiles map[string]models.SkillAuthenticationProfiles `yaml:"authenticationProfiles,omitempty"`
	Secrets                []*models.SkillSecret                         `yaml:"secrets,omitempty"`
	Tools                  []*models.SkillTool                           `yaml:"tools,omitempty"`
}

func parseFrontmatter(data []byte) (*libraryFrontmatter, string, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return nil, "", fmt.Errorf("missing frontmatter delimiter")
	}
	rest := content[4:]
	closingIndex := strings.Index(rest, "\n---\n")
	if closingIndex < 0 {
		if strings.HasSuffix(rest, "\n---") {
			closingIndex = len(rest) - 4
		} else {
			return nil, "", fmt.Errorf("missing closing frontmatter delimiter")
		}
	}
	frontmatterYAML := rest[:closingIndex]
	var parsed libraryFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatterYAML), &parsed); err != nil {
		return nil, "", fmt.Errorf("parsing frontmatter: %w", err)
	}
	bodyStart := closingIndex + 5
	prompt := ""
	if bodyStart <= len(rest) {
		prompt = strings.TrimSpace(rest[bodyStart:])
	}
	return &parsed, prompt, nil
}
