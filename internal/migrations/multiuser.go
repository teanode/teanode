package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teanode/teanode/internal/configs"
	"github.com/teanode/teanode/internal/util/atomicfile"
	"github.com/teanode/teanode/internal/util/security"
	"github.com/teanode/teanode/internal/util/timeutil"
	"github.com/teanode/teanode/internal/util/trash"
	"gopkg.in/yaml.v3"
)

const multiUserMigrationMarker = "multiuser-v2.done"

type legacyState struct {
	DefaultAgentId         string            `yaml:"defaultAgentId,omitempty"`
	DefaultConversationIds map[string]string `yaml:"defaultConversationIds,omitempty"`
	Users                  map[string]struct {
		DefaultAgentId         string            `yaml:"defaultAgentId,omitempty"`
		DefaultConversationIds map[string]string `yaml:"defaultConversationIds,omitempty"`
	} `yaml:"users,omitempty"`
}

type legacySecurityFile struct {
	Version      int                             `yaml:"version,omitempty"`
	Users        map[string]configs.SecurityUser `yaml:"users,omitempty"`
	ChannelLinks configs.ChannelLinks            `yaml:"channelLinks,omitempty"`
	Token        string                          `yaml:"token,omitempty"`
	Password     string                          `yaml:"password,omitempty"`
}

func MigrateMultiUserV2() error {
	directory, err := configs.Directory()
	if err != nil {
		return err
	}
	migrationsDirectory := filepath.Join(directory, ".migrations")
	if err := os.MkdirAll(migrationsDirectory, 0755); err != nil {
		return err
	}
	markerPath := filepath.Join(migrationsDirectory, multiUserMigrationMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	}

	if err := backupIfExists(filepath.Join(directory, "security.yaml"), filepath.Join(directory, ".backup")); err != nil {
		return err
	}
	if err := backupIfExists(filepath.Join(directory, "state.yaml"), filepath.Join(directory, ".backup")); err != nil {
		return err
	}

	securityConfig, legacyToken, legacyPasswordHash, err := loadSecurityForMigration()
	if err != nil {
		return err
	}
	initialUserId := firstUserId(securityConfig)
	if initialUserId == "" {
		initialUserId = security.NewULID()
	}
	if len(securityConfig.Users) == 0 {
		initialUsername := configs.OSUsername()
		initialPassword := legacyPasswordHash
		tokens := []configs.SecurityToken{}
		if legacyToken != "" {
			tokens = append(tokens, configs.SecurityToken{
				ID:        security.NewULID(),
				Token:     legacyToken,
				CreatedAt: time.Now(),
			})
		}
		securityConfig.Users = map[string]configs.SecurityUser{
			initialUserId: {
				Username:     initialUsername,
				Admin:        true,
				PasswordHash: initialPassword,
				Tokens:       tokens,
			},
		}
		securityConfig.ChannelLinks = configs.ChannelLinks{
			Telegram: map[string]string{},
			Discord:  map[string]string{},
		}
	}
	if err := configs.SaveSecurity(securityConfig); err != nil {
		return err
	}

	if err := migrateState(initialUserId); err != nil {
		return err
	}
	if err := configs.EnsureUserDirectories(initialUserId); err != nil {
		return err
	}
	if err := migrateAgentWorkspaces(directory); err != nil {
		return err
	}
	if err := migrateProjectWorkspaces(directory); err != nil {
		return err
	}
	if err := migrateLegacyUserData(directory, initialUserId); err != nil {
		return err
	}
	if err := migrateLegacyJobs(directory, initialUserId); err != nil {
		return err
	}
	if err := atomicfile.WriteFile(markerPath, []byte(timeutil.Now().String()+"\n")); err != nil {
		return err
	}
	return nil
}

func loadSecurityForMigration() (*configs.SecurityConfig, string, string, error) {
	securityFile, err := configs.SecurityFile()
	if err != nil {
		return nil, "", "", err
	}

	raw := &legacySecurityFile{}
	data, err := os.ReadFile(securityFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &configs.SecurityConfig{}, "", "", nil
		}
		return nil, "", "", fmt.Errorf("reading security config: %w", err)
	}
	if err := yaml.Unmarshal(data, raw); err != nil {
		return nil, "", "", fmt.Errorf("parsing security config: %w", err)
	}

	config := &configs.SecurityConfig{
		Users:        raw.Users,
		ChannelLinks: raw.ChannelLinks,
	}
	if config.Users == nil {
		config.Users = map[string]configs.SecurityUser{}
	}
	if config.ChannelLinks.Telegram == nil {
		config.ChannelLinks.Telegram = map[string]string{}
	}
	if config.ChannelLinks.Discord == nil {
		config.ChannelLinks.Discord = map[string]string{}
	}
	return config, strings.TrimSpace(raw.Token), strings.TrimSpace(raw.Password), nil
}

func firstUserId(securityConfig *configs.SecurityConfig) string {
	if securityConfig == nil || len(securityConfig.Users) == 0 {
		return ""
	}
	userIds := make([]string, 0, len(securityConfig.Users))
	for userId := range securityConfig.Users {
		userIds = append(userIds, userId)
	}
	sort.Strings(userIds)
	return userIds[0]
}

func migrateState(initialUserId string) error {
	stateFile, err := configs.StateFile()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			state := map[string]interface{}{
				"users": map[string]interface{}{
					initialUserId: map[string]interface{}{},
				},
			}
			encoded, marshalErr := yaml.Marshal(state)
			if marshalErr != nil {
				return marshalErr
			}
			return atomicfile.WriteFile(stateFile, encoded)
		}
		return err
	}
	var legacy legacyState
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if len(legacy.Users) > 0 {
		return nil
	}
	users := map[string]interface{}{
		initialUserId: map[string]interface{}{
			"defaultAgentId":         legacy.DefaultAgentId,
			"defaultConversationIds": legacy.DefaultConversationIds,
		},
	}
	state := map[string]interface{}{
		"users": users,
	}
	encoded, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(stateFile, encoded)
}

func migrateAgentWorkspaces(root string) error {
	legacyRoots := []string{
		filepath.Join(root, "workspace"),
		filepath.Join(root, "workspaces"),
	}
	for _, legacyRoot := range legacyRoots {
		entries, err := os.ReadDir(legacyRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			agentId := entry.Name()
			source := filepath.Join(legacyRoot, agentId)
			destination, err := configs.AgentWorkspaceDirectory(agentId)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			if _, err := os.Stat(destination); err == nil {
				continue
			}
			if err := os.Rename(source, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateProjectWorkspaces(root string) error {
	projectsDirectory := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsDirectory)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			projectId := entry.Name()
			source := filepath.Join(projectsDirectory, projectId)
			destination := filepath.Join(projectsDirectory, projectId, "workspace")
			if source == destination {
				continue
			}
			if err := os.MkdirAll(destination, 0755); err != nil {
				return err
			}
			items, readErr := os.ReadDir(source)
			if readErr != nil {
				return readErr
			}
			for _, item := range items {
				if item.Name() == "workspace" || item.Name() == "project.yaml" {
					continue
				}
				from := filepath.Join(source, item.Name())
				to := filepath.Join(destination, item.Name())
				if _, err := os.Stat(to); err == nil {
					continue
				}
				if err := os.Rename(from, to); err != nil {
					return err
				}
			}
			continue
		}
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		projectId := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		targetDirectory := filepath.Join(projectsDirectory, projectId)
		if err := os.MkdirAll(targetDirectory, 0755); err != nil {
			return err
		}
		source := filepath.Join(projectsDirectory, entry.Name())
		target := filepath.Join(targetDirectory, "project.yaml")
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyUserData(root, userId string) error {
	userWorkspace, err := configs.UserWorkspaceDirectory(userId)
	if err != nil {
		return err
	}
	userProfile, err := configs.UserProfileFile(userId)
	if err != nil {
		return err
	}
	legacyProfile := filepath.Join(root, "profile.md")
	if _, err := os.Stat(legacyProfile); err == nil {
		if _, dstErr := os.Stat(userProfile); os.IsNotExist(dstErr) {
			if err := migrateLegacyProfileFile(legacyProfile, userId); err != nil {
				return err
			}
		} else {
			trashDirectory, trashErr := configs.TrashDirectory()
			if trashErr == nil {
				_ = trash.Move(legacyProfile, trashDirectory)
			}
		}
	}
	legacyUserProfile := filepath.Join(root, "users", userId+".md")
	if _, err := os.Stat(legacyUserProfile); err == nil {
		if _, dstErr := os.Stat(userProfile); os.IsNotExist(dstErr) {
			if err := migrateLegacyProfileFile(legacyUserProfile, userId); err != nil {
				return err
			}
		} else {
			trashDirectory, trashErr := configs.TrashDirectory()
			if trashErr == nil {
				_ = trash.Move(legacyUserProfile, trashDirectory)
			}
		}
	}

	legacyConversationRoot := filepath.Join(root, "conversations")
	agentEntries, err := os.ReadDir(legacyConversationRoot)
	if err == nil {
		for _, agentEntry := range agentEntries {
			if !agentEntry.IsDir() {
				continue
			}
			source := filepath.Join(legacyConversationRoot, agentEntry.Name())
			destination, err := configs.UserAgentConversationsDirectory(userId, agentEntry.Name())
			if err != nil {
				return err
			}
			if err := os.MkdirAll(destination, 0755); err != nil {
				return err
			}
			items, readErr := os.ReadDir(source)
			if readErr != nil {
				continue
			}
			for _, item := range items {
				if item.IsDir() || filepath.Ext(item.Name()) != ".jsonl" {
					continue
				}
				from := filepath.Join(source, item.Name())
				to := filepath.Join(destination, item.Name())
				if _, err := os.Stat(to); err == nil {
					continue
				}
				if err := os.Rename(from, to); err != nil {
					return err
				}
			}
		}
	}

	legacyWorkspace := filepath.Join(root, "workspace")
	workspaceEntries, err := os.ReadDir(legacyWorkspace)
	if err == nil {
		for _, entry := range workspaceEntries {
			source := filepath.Join(legacyWorkspace, entry.Name())
			if entry.IsDir() {
				continue
			}
			destination := filepath.Join(userWorkspace, entry.Name())
			if _, err := os.Stat(destination); err == nil {
				trashDirectory, _ := configs.TrashDirectory()
				_ = trash.Move(source, trashDirectory)
				continue
			}
			if err := os.Rename(source, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateLegacyJobs(root, userId string) error {
	sourceDirectory := filepath.Join(root, "jobs")
	entries, err := os.ReadDir(sourceDirectory)
	if err != nil {
		return nil
	}
	destinationDirectory, err := configs.UserJobsDirectory(userId)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destinationDirectory, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		source := filepath.Join(sourceDirectory, entry.Name())
		destination := filepath.Join(destinationDirectory, entry.Name())
		if _, err := os.Stat(destination); err == nil {
			continue
		}
		if err := os.Rename(source, destination); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyProfileFile(path, userId string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	profile := parseLegacyProfile(data)
	if err := configs.SaveUserProfile(userId, profile); err != nil {
		return err
	}
	return os.Remove(path)
}

func parseLegacyProfile(data []byte) *configs.UserProfile {
	type profileYaml struct {
		Name          string `yaml:"name"`
		Description   string `yaml:"description"`
		Bio           string `yaml:"bio"`
		AvatarMediaID string `yaml:"avatarMediaId"`
	}
	resolveDescription := func(description, bio, body string) string {
		trimmedDescription := strings.TrimSpace(description)
		if trimmedDescription != "" {
			return trimmedDescription
		}
		trimmedBio := strings.TrimSpace(bio)
		if trimmedBio != "" {
			return trimmedBio
		}
		return strings.TrimSpace(body)
	}
	var parsed profileYaml
	if err := yaml.Unmarshal(data, &parsed); err == nil {
		description := resolveDescription(parsed.Description, parsed.Bio, "")
		if strings.TrimSpace(parsed.Name) != "" || strings.TrimSpace(parsed.AvatarMediaID) != "" || description != "" {
			return &configs.UserProfile{
				Name:          strings.TrimSpace(parsed.Name),
				Description:   description,
				AvatarMediaID: strings.TrimSpace(parsed.AvatarMediaID),
			}
		}
	}

	content := string(data)
	if strings.HasPrefix(content, "---\n") {
		rest := content[len("---\n"):]
		endIndex := strings.Index(rest, "\n---")
		if endIndex >= 0 {
			frontMatter := rest[:endIndex]
			if err := yaml.Unmarshal([]byte(frontMatter), &parsed); err == nil {
				description := resolveDescription(parsed.Description, parsed.Bio, rest[endIndex+len("\n---"):])
				return &configs.UserProfile{
					Name:          strings.TrimSpace(parsed.Name),
					Description:   description,
					AvatarMediaID: strings.TrimSpace(parsed.AvatarMediaID),
				}
			}
		}
	}
	return &configs.UserProfile{Description: strings.TrimSpace(content)}
}

func backupIfExists(path, backupDirectory string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(backupDirectory, 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	target := filepath.Join(backupDirectory, fmt.Sprintf("%s.%d.bak", filepath.Base(path), time.Now().Unix()))
	return atomicfile.WriteFile(target, data)
}
