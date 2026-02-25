package skills

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/timeutil"
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
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Version     string             `json:"version"`
	Enabled     bool               `json:"enabled"`
	SourceID    string             `json:"sourceId,omitempty"`
	Publisher   string             `json:"publisher,omitempty"`
	InstalledAt timeutil.Timestamp `json:"installedAt,omitempty"`
}

type installManifest struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Version     string             `json:"version"`
	Enabled     *bool              `json:"enabled,omitempty"`
	SourceID    string             `json:"sourceId"`
	Publisher   string             `json:"publisher"`
	Digest      string             `json:"digest"`
	InstalledAt timeutil.Timestamp `json:"installedAt"`
}

func fetchIndex(ctx context.Context, source configs.SkillsRegistry) (*Index, error) {
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

func VerifyEntrySignature(entry SkillEntry, source configs.SkillsRegistry) error {
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
		publicKey, parseError := parseEd25519PublicKey(keyText)
		if parseError != nil {
			continue
		}
		if ed25519.Verify(publicKey, message, signature) {
			return nil
		}
	}
	return fmt.Errorf("signature verification failed")
}

func parseEd25519PublicKey(keyText string) (ed25519.PublicKey, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(keyText))
	if err != nil {
		return nil, err
	}

	// Raw 32-byte Ed25519 public key.
	if len(keyBytes) == ed25519.PublicKeySize {
		return ed25519.PublicKey(keyBytes), nil
	}

	// PKIX/DER SubjectPublicKeyInfo form (commonly copied from PEM body).
	parsed, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		return nil, err
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ed25519")
	}
	return publicKey, nil
}

func Search(ctx context.Context, registries []configs.SkillsRegistry, query string) ([]SearchResult, error) {
	if len(registries) == 0 {
		return nil, nil
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var results []SearchResult
	for _, source := range registries {
		index, err := fetchIndex(ctx, source)
		if err != nil {
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

func findEntry(ctx context.Context, registries []configs.SkillsRegistry, sourceId string, name string, version string) (*SkillEntry, configs.SkillsRegistry, string, error) {
	if len(registries) == 0 {
		return nil, configs.SkillsRegistry{}, "", fmt.Errorf("skills registry is not configured")
	}

	var chosen *SkillEntry
	var chosenSource configs.SkillsRegistry
	chosenPublisher := ""

	for _, source := range registries {
		if sourceId != "" && source.ID != sourceId {
			continue
		}
		index, err := fetchIndex(ctx, source)
		if err != nil {
			continue
		}
		for _, entry := range index.Skills {
			if entry.Name != name {
				continue
			}
			if version != "" && entry.Version != version {
				continue
			}
			if chosen == nil || compareRegistryVersions(entry.Version, chosen.Version) > 0 {
				copyEntry := entry
				chosen = &copyEntry
				chosenSource = source
				chosenPublisher = index.Publisher
			}
		}
	}

	if chosen == nil {
		return nil, configs.SkillsRegistry{}, "", fmt.Errorf("skill not found: %s", name)
	}
	return chosen, chosenSource, chosenPublisher, nil
}

func Install(ctx context.Context, registries []configs.SkillsRegistry, sourceId string, name string, version string) (*InstalledSkill, error) {
	entry, source, publisher, err := findEntry(ctx, registries, sourceId, name, version)
	if err != nil {
		return nil, err
	}
	if !source.IgnoreSignatures {
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
	definition := SkillDefinition{}
	body, parseError := parseSkillMarkdown(data, &definition)
	if parseError != nil {
		return nil, parseError
	}
	definition.Prompt = strings.TrimSpace(body)
	if strings.TrimSpace(definition.Name) == "" {
		definition.Name = strings.TrimSpace(entry.Name)
	}
	if strings.TrimSpace(definition.Description) == "" {
		definition.Description = strings.TrimSpace(entry.Description)
	}
	if strings.TrimSpace(definition.Name) == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	toolsData, marshalToolsError := json.Marshal(definition.Tools)
	if marshalToolsError != nil {
		return nil, marshalToolsError
	}
	tools := make([]map[string]interface{}, 0)
	if unmarshalToolsError := json.Unmarshal(toolsData, &tools); unmarshalToolsError != nil {
		return nil, unmarshalToolsError
	}
	httpAuthData, marshalHTTPAuthError := json.Marshal(definition.HTTPAuth)
	if marshalHTTPAuthError != nil {
		return nil, marshalHTTPAuthError
	}
	httpAuth := map[string]interface{}{}
	if unmarshalHTTPAuthError := json.Unmarshal(httpAuthData, &httpAuth); unmarshalHTTPAuthError != nil {
		return nil, unmarshalHTTPAuthError
	}

	now := timeutil.Now()
	metadata := map[string]interface{}{
		"description": definition.Description,
		"enabled":     true,
		"sourceId":    source.ID,
		"publisher":   publisher,
		"digest":      digest,
		"installedAt": now.String(),
	}
	skillStore, closeStore, storeError := openConfiguredSkillStore(ctx)
	if storeError != nil {
		return nil, storeError
	}
	defer closeStore()
	upsertError := skillStore.Transaction(func(transaction store.Transaction) error {
		existingSkill, getError := transaction.GetSkill(definition.Name, nil)
		if getError != nil && getError != store.ErrNotFound {
			return getError
		}
		versionText := strings.TrimSpace(entry.Version)
		if versionText == "" {
			versionText = strings.TrimSpace(definition.RuntimeMinVersion)
		}
		if versionText == "" {
			versionText = "0.0.0"
		}
		if getError == store.ErrNotFound {
			_, createError := transaction.CreateSkill(&models.Skill{
				ID:                definition.Name,
				Name:              &definition.Name,
				Description:       &definition.Description,
				Version:           &versionText,
				RuntimeMinVersion: &definition.RuntimeMinVersion,
				HTTPAuth:          &httpAuth,
				Tools:             &tools,
				Enabled:           ptrBool(true),
				Source:            &source.ID,
				Publisher:         &publisher,
				Metadata:          &metadata,
				Prompt:            &definition.Prompt,
			}, nil)
			return createError
		}
		_, modifyError := transaction.ModifySkill(existingSkill.ID, func(skill *models.Skill) error {
			skill.Name = &definition.Name
			skill.Description = &definition.Description
			skill.Version = &versionText
			skill.RuntimeMinVersion = &definition.RuntimeMinVersion
			skill.HTTPAuth = &httpAuth
			skill.Tools = &tools
			skill.Enabled = ptrBool(true)
			skill.Source = &source.ID
			skill.Publisher = &publisher
			skill.Metadata = &metadata
			skill.Prompt = &definition.Prompt
			return nil
		}, nil)
		return modifyError
	})
	if upsertError != nil {
		return nil, upsertError
	}
	return &InstalledSkill{
		Name:        definition.Name,
		Description: definition.Description,
		Version:     entry.Version,
		Enabled:     true,
		SourceID:    source.ID,
		Publisher:   publisher,
		InstalledAt: now,
	}, nil
}

func ListInstalled(ctx context.Context) ([]InstalledSkill, error) {
	skillStore, closeStore, storeError := openConfiguredSkillStore(ctx)
	if storeError != nil {
		return nil, storeError
	}
	defer closeStore()
	installed := make([]InstalledSkill, 0)
	listError := skillStore.Transaction(func(transaction store.Transaction) error {
		skills, getError := transaction.ListSkills(nil)
		if getError != nil {
			return getError
		}
		for _, skill := range skills {
			record := InstalledSkill{
				Name:    firstNonEmpty(strings.TrimSpace(skill.ID), strings.TrimSpace(valueOrEmptyString(skill.Name))),
				Version: strings.TrimSpace(valueOrEmptyString(skill.Version)),
				Enabled: true,
			}
			if skill.Metadata != nil {
				record.Description = metadataString(*skill.Metadata, "description")
				record.SourceID = metadataString(*skill.Metadata, "sourceId")
				if record.SourceID == "" {
					record.SourceID = metadataString(*skill.Metadata, "source")
				}
				record.Publisher = metadataString(*skill.Metadata, "publisher")
				if record.Publisher == "" {
					record.Publisher = metadataString(*skill.Metadata, "sourcePublisher")
				}
				if enabledValue, ok := (*skill.Metadata)["enabled"].(bool); ok {
					record.Enabled = enabledValue
				}
				installedAtText := metadataString(*skill.Metadata, "installedAt")
				if installedAtText != "" {
					if parsedTimestamp, parseError := timeutil.Parse(installedAtText); parseError == nil {
						record.InstalledAt = parsedTimestamp
					}
				}
			}
			if record.Description == "" {
				record.Description = strings.TrimSpace(valueOrEmptyString(skill.Name))
			}
			installed = append(installed, record)
		}
		return nil
	})
	if listError != nil {
		return nil, listError
	}
	return installed, nil
}

func SetInstalledSkillEnabled(ctx context.Context, name string, enabled bool) error {
	skillStore, closeStore, storeError := openConfiguredSkillStore(ctx)
	if storeError != nil {
		return storeError
	}
	defer closeStore()
	return skillStore.Transaction(func(transaction store.Transaction) error {
		_, modifyError := transaction.ModifySkill(strings.TrimSpace(name), func(skill *models.Skill) error {
			metadata := map[string]interface{}{}
			if skill.Metadata != nil {
				for key, value := range *skill.Metadata {
					metadata[key] = value
				}
			}
			metadata["enabled"] = enabled
			skill.Metadata = &metadata
			return nil
		}, nil)
		if modifyError == store.ErrNotFound {
			return fmt.Errorf("skill not installed: %s", name)
		}
		return modifyError
	})
}

func Uninstall(ctx context.Context, name string) error {
	skillStore, closeStore, storeError := openConfiguredSkillStore(ctx)
	if storeError != nil {
		return storeError
	}
	defer closeStore()
	deleteError := skillStore.Transaction(func(transaction store.Transaction) error {
		return transaction.DeleteSkill(strings.TrimSpace(name), nil)
	})
	if deleteError == store.ErrNotFound {
		return fmt.Errorf("skill not installed: %s", name)
	}
	return deleteError
}

func Update(ctx context.Context, registries []configs.SkillsRegistry, name string) ([]InstalledSkill, error) {
	installed, err := ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	var updated []InstalledSkill
	for _, item := range installed {
		if name != "" && item.Name != name {
			continue
		}
		registry := findRegistryById(registries, item.SourceID)
		if registry != nil && registry.IgnoreUpdates {
			continue
		}
		installedItem, installErr := Install(ctx, registries, item.SourceID, item.Name, "")
		if installErr != nil {
			continue
		}
		if compareRegistryVersions(installedItem.Version, item.Version) > 0 {
			updated = append(updated, *installedItem)
		}
	}
	return updated, nil
}

func findRegistryById(registries []configs.SkillsRegistry, id string) *configs.SkillsRegistry {
	for index := range registries {
		if registries[index].ID == id {
			return &registries[index]
		}
	}
	return nil
}

func compareRegistryVersions(left string, right string) int {
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

func openConfiguredSkillStore(ctx context.Context) (store.Store, func(), error) {
	skillStore := store.StoreFromContext(ctx)
	return skillStore, func() {}, nil
}

func metadataString(metadata map[string]interface{}, key string) string {
	value, exists := metadata[key]
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
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func valueOrEmptyString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func ptrBool(value bool) *bool {
	return &value
}
