package registry

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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
)

type Index struct {
	Publisher string       `json:"publisher"`
	Skills    []SkillEntry `json:"skills"`
}

type SkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version"`
	URL         string   `json:"url"`
	SHA256      string   `json:"sha256"`
	Signature   string   `json:"signature,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type SearchResult struct {
	SourceID    string   `json:"sourceId"`
	Publisher   string   `json:"publisher"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags,omitempty"`
}

type InstalledSkill struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	SourceID    string `json:"sourceId,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	InstalledAt int64  `json:"installedAt,omitempty"`
}

type installManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	SourceID    string `json:"sourceId"`
	Publisher   string `json:"publisher"`
	Digest      string `json:"digest"`
	InstalledAt int64  `json:"installedAt"`
}

func fetchIndex(ctx context.Context, source configs.SkillsRegistrySource) (*Index, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.IndexURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("index request failed: HTTP %d", response.StatusCode)
	}
	var index Index
	if err := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&index); err != nil {
		return nil, fmt.Errorf("parsing index: %w", err)
	}
	if index.Publisher == "" {
		index.Publisher = source.Publisher
	}
	return &index, nil
}

func publisherAllowed(policy configs.SkillsRegistryPolicy, publisher string) bool {
	if len(policy.AllowPublishers) == 0 {
		return true
	}
	for _, allowed := range policy.AllowPublishers {
		if allowed == publisher {
			return true
		}
	}
	return false
}

func VerifyEntrySignature(entry SkillEntry, source configs.SkillsRegistrySource) error {
	if entry.Signature == "" {
		return fmt.Errorf("missing signature")
	}
	if len(source.PublicKeys) == 0 {
		return fmt.Errorf("no public keys configured for source %s", source.ID)
	}
	signature, err := base64.StdEncoding.DecodeString(entry.Signature)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	message := []byte(fmt.Sprintf("%s\n%s\n%s\n%s", entry.Name, entry.Version, entry.URL, strings.ToLower(entry.SHA256)))
	for _, keyText := range source.PublicKeys {
		keyBytes, decodeErr := base64.StdEncoding.DecodeString(keyText)
		if decodeErr != nil {
			continue
		}
		if len(keyBytes) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(keyBytes), message, signature) {
			return nil
		}
	}
	return fmt.Errorf("signature verification failed")
}

func Search(ctx context.Context, config *configs.SkillsRegistryConfig, query string) ([]SearchResult, error) {
	if config == nil || !config.Enabled {
		return nil, nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var results []SearchResult
	for _, source := range config.Sources {
		if !source.Enabled {
			continue
		}
		index, err := fetchIndex(ctx, source)
		if err != nil {
			continue
		}
		if !publisherAllowed(config.Policy, index.Publisher) {
			continue
		}
		for _, entry := range index.Skills {
			if query != "" {
				matched := strings.Contains(strings.ToLower(entry.Name), query) ||
					strings.Contains(strings.ToLower(entry.Description), query)
				if !matched {
					for _, tag := range entry.Tags {
						if strings.Contains(strings.ToLower(tag), query) {
							matched = true
							break
						}
					}
				}
				if !matched {
					continue
				}
			}
			results = append(results, SearchResult{
				SourceID:    source.ID,
				Publisher:   index.Publisher,
				Name:        entry.Name,
				Description: entry.Description,
				Version:     entry.Version,
				Tags:        entry.Tags,
			})
		}
	}
	return results, nil
}

func findEntry(ctx context.Context, config *configs.SkillsRegistryConfig, sourceID string, name string, version string) (*SkillEntry, configs.SkillsRegistrySource, string, error) {
	if config == nil || !config.Enabled {
		return nil, configs.SkillsRegistrySource{}, "", fmt.Errorf("skills registry is disabled")
	}

	var chosen *SkillEntry
	var chosenSource configs.SkillsRegistrySource
	chosenPublisher := ""

	for _, source := range config.Sources {
		if !source.Enabled {
			continue
		}
		if sourceID != "" && source.ID != sourceID {
			continue
		}
		index, err := fetchIndex(ctx, source)
		if err != nil {
			continue
		}
		if !publisherAllowed(config.Policy, index.Publisher) {
			continue
		}
		for _, entry := range index.Skills {
			if entry.Name != name {
				continue
			}
			if version != "" && entry.Version != version {
				continue
			}
			if chosen == nil || compareVersions(entry.Version, chosen.Version) > 0 {
				copyEntry := entry
				chosen = &copyEntry
				chosenSource = source
				chosenPublisher = index.Publisher
			}
		}
	}

	if chosen == nil {
		return nil, configs.SkillsRegistrySource{}, "", fmt.Errorf("skill not found: %s", name)
	}
	return chosen, chosenSource, chosenPublisher, nil
}

func Install(ctx context.Context, config *configs.SkillsRegistryConfig, sourceID string, name string, version string) (*InstalledSkill, error) {
	entry, source, publisher, err := findEntry(ctx, config, sourceID, name, version)
	if err != nil {
		return nil, err
	}
	if config.Policy.RequireSignatures {
		if err := VerifyEntrySignature(*entry, source); err != nil {
			return nil, err
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed: HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	digest := strings.ToLower(hex.EncodeToString(sum[:]))
	if digest != strings.ToLower(entry.SHA256) {
		return nil, fmt.Errorf("digest mismatch")
	}

	skillsDirectory, err := configs.SkillsDirectory()
	if err != nil {
		return nil, err
	}
	installDir, err := resolveInstallDir(skillsDirectory, entry.Name, entry.Version)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return nil, err
	}
	if err := ensureNoSymlinkComponents(resolveInstalledRoot(skillsDirectory), installDir); err != nil {
		return nil, err
	}
	if err := writeInstalledFile(installDir, "skill.md", data); err != nil {
		return nil, err
	}
	manifest := installManifest{
		Name:        entry.Name,
		Version:     entry.Version,
		SourceID:    source.ID,
		Publisher:   publisher,
		Digest:      digest,
		InstalledAt: time.Now().UnixMilli(),
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeInstalledFile(installDir, "manifest.json", manifestBytes); err != nil {
		return nil, err
	}
	notifyInstalledSkillsChanged(skillsDirectory)
	return &InstalledSkill{
		Name:        entry.Name,
		Version:     entry.Version,
		SourceID:    source.ID,
		Publisher:   publisher,
		InstalledAt: manifest.InstalledAt,
	}, nil
}

func ListInstalled() ([]InstalledSkill, error) {
	skillsDirectory, err := configs.SkillsDirectory()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(skillsDirectory, ".installed")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var installed []InstalledSkill
	for _, skillEntry := range entries {
		if !skillEntry.IsDir() {
			continue
		}
		if !isSafePathSegment(skillEntry.Name()) {
			continue
		}
		versionEntries, err := os.ReadDir(filepath.Join(root, skillEntry.Name()))
		if err != nil {
			continue
		}

		bestVersion := ""
		bestPath := ""
		for _, versionEntry := range versionEntries {
			if !versionEntry.IsDir() {
				continue
			}
			version := versionEntry.Name()
			if !isSafePathSegment(version) {
				continue
			}
			if bestVersion == "" || compareVersions(version, bestVersion) > 0 {
				bestVersion = version
				bestPath = filepath.Join(root, skillEntry.Name(), version, "manifest.json")
			}
		}
		if bestVersion == "" {
			continue
		}

		record := InstalledSkill{Name: skillEntry.Name(), Version: bestVersion}
		if data, err := os.ReadFile(bestPath); err == nil {
			var manifest installManifest
			if json.Unmarshal(data, &manifest) == nil {
				record.SourceID = manifest.SourceID
				record.Publisher = manifest.Publisher
				record.InstalledAt = manifest.InstalledAt
			}
		}
		installed = append(installed, record)
	}
	return installed, nil
}

func Uninstall(name string) error {
	skillsDirectory, err := configs.SkillsDirectory()
	if err != nil {
		return err
	}
	if !isSafePathSegment(name) {
		return fmt.Errorf("invalid skill name")
	}
	path, err := resolveInstalledSkillPath(skillsDirectory, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("skill not installed: %s", name)
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	notifyInstalledSkillsChanged(skillsDirectory)
	return nil
}

func Update(ctx context.Context, config *configs.SkillsRegistryConfig, name string) ([]InstalledSkill, error) {
	installed, err := ListInstalled()
	if err != nil {
		return nil, err
	}
	var updated []InstalledSkill
	for _, item := range installed {
		if name != "" && item.Name != name {
			continue
		}
		installedItem, installErr := Install(ctx, config, item.SourceID, item.Name, "")
		if installErr != nil {
			continue
		}
		if compareVersions(installedItem.Version, item.Version) > 0 {
			updated = append(updated, *installedItem)
		}
	}
	return updated, nil
}

func compareVersions(left string, right string) int {
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

func isSafePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	clean := filepath.Clean(value)
	return clean == value
}

func resolveInstalledRoot(skillsDirectory string) string {
	return filepath.Join(skillsDirectory, ".installed")
}

func resolveInstalledSkillPath(skillsDirectory, name string) (string, error) {
	if !isSafePathSegment(name) {
		return "", fmt.Errorf("invalid skill name")
	}
	root := resolveInstalledRoot(skillsDirectory)
	path := filepath.Join(root, name)
	if err := ensureWithinRoot(root, path); err != nil {
		return "", err
	}
	if err := ensureNoSymlinkComponents(root, path); err != nil {
		return "", err
	}
	return path, nil
}

func resolveInstallDir(skillsDirectory, name, version string) (string, error) {
	if !isSafePathSegment(name) {
		return "", fmt.Errorf("invalid skill name")
	}
	if !isSafePathSegment(version) {
		return "", fmt.Errorf("invalid skill version")
	}
	root := resolveInstalledRoot(skillsDirectory)
	path := filepath.Join(root, name, version)
	if err := ensureWithinRoot(root, path); err != nil {
		return "", err
	}
	if err := ensureNoSymlinkComponents(root, path); err != nil {
		return "", err
	}
	return path, nil
}

func ensureWithinRoot(root string, path string) error {
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid path")
	}
	return nil
}

func ensureNoSymlinkComponents(root string, path string) error {
	info, err := os.Lstat(root)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid path")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(relativePath, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		componentInfo, componentErr := os.Lstat(current)
		if componentErr != nil {
			if os.IsNotExist(componentErr) {
				continue
			}
			return componentErr
		}
		if componentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid path")
		}
	}
	return nil
}

func notifyInstalledSkillsChanged(skillsDirectory string) {
	root := resolveInstalledRoot(skillsDirectory)
	if err := os.MkdirAll(root, 0755); err != nil {
		return
	}
	// Touch a marker file so filesystem watchers pick up installs/updates/removals immediately.
	_ = os.WriteFile(filepath.Join(root, ".reload"), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)), 0644)
}
