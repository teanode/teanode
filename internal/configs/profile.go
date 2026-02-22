package configs

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teanode/teanode/internal/util/atomicfile"
	"gopkg.in/yaml.v3"
)

// Profile stores user-facing identity information for prompt personalization.
type Profile struct {
	Name          string `json:"name" yaml:"name"`
	Bio           string `json:"bio,omitempty" yaml:"bio,omitempty"`
	AvatarMediaID string `json:"avatarMediaId,omitempty" yaml:"avatarMediaId,omitempty"`
}

type saveProfileOptions struct {
	overwriteBio bool
}

const (
	profileFrontMatterDelimiter = "---"
	profileNameKey              = "name"
	profileAvatarMediaIDKey     = "avatarMediaId"
)

// ProfileFile returns the path to ~/.teanode/profile.md.
func ProfileFile() (string, error) {
	directory, err := Directory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "profile.md"), nil
}

// OSUsername returns the current process username, falling back to environment
// variables and finally "teanode" if unavailable.
func OSUsername() string {
	if current, err := user.Current(); err == nil {
		name := strings.TrimSpace(current.Username)
		if name != "" {
			return name
		}
	}
	for _, key := range []string{"USER", "USERNAME"} {
		name := strings.TrimSpace(os.Getenv(key))
		if name != "" {
			return name
		}
	}
	return "teanode"
}

// LoadProfile reads profile.md and applies a name fallback to OS username
// when the file is missing or name is empty.
func LoadProfile() (*Profile, error) {
	profileFile, err := ProfileFile()
	if err != nil {
		return nil, err
	}

	profile := &Profile{}
	data, err := os.ReadFile(profileFile)
	if err != nil {
		if os.IsNotExist(err) {
			profile.Name = OSUsername()
			return profile, nil
		}
		return nil, fmt.Errorf("reading profile config: %w", err)
	}

	frontMatter, body, err := parseProfileMarkdown(data)
	if err != nil {
		return nil, fmt.Errorf("parsing profile config: %w", err)
	}

	profile.Name = strings.TrimSpace(frontMatter[profileNameKey])
	profile.AvatarMediaID = strings.TrimSpace(frontMatter[profileAvatarMediaIDKey])
	profile.Bio = body
	if profile.Name == "" {
		profile.Name = OSUsername()
	}
	return profile, nil
}

// SaveProfile writes profile.md atomically with strict file permissions.
func SaveProfile(profile *Profile) error {
	return saveProfile(profile, saveProfileOptions{})
}

// SaveProfileOverwriteBio writes profile.md and always uses profile.Bio as the
// persisted markdown body, including when profile.Bio is empty.
func SaveProfileOverwriteBio(profile *Profile) error {
	return saveProfile(profile, saveProfileOptions{overwriteBio: true})
}

func saveProfile(profile *Profile, options saveProfileOptions) error {
	if profile == nil {
		return fmt.Errorf("profile is required")
	}

	profileFile, err := ProfileFile()
	if err != nil {
		return err
	}

	normalized := &Profile{
		Name:          strings.TrimSpace(profile.Name),
		Bio:           profile.Bio,
		AvatarMediaID: strings.TrimSpace(profile.AvatarMediaID),
	}
	if normalized.Name == "" {
		normalized.Name = OSUsername()
	}

	existingFrontMatter := map[string]string{}
	existingBody := ""
	if existingData, err := os.ReadFile(profileFile); err == nil {
		if parsedFrontMatter, parsedBody, parseErr := parseProfileMarkdown(existingData); parseErr == nil {
			existingFrontMatter = parsedFrontMatter
			existingBody = parsedBody
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading existing profile config: %w", err)
	}
	if !options.overwriteBio && normalized.Bio == "" {
		normalized.Bio = existingBody
	}

	frontMatter := make(map[string]string, len(existingFrontMatter)+2)
	for key, value := range existingFrontMatter {
		if key == profileNameKey || key == profileAvatarMediaIDKey {
			continue
		}
		frontMatter[key] = value
	}
	frontMatter[profileNameKey] = normalized.Name
	if normalized.AvatarMediaID != "" {
		frontMatter[profileAvatarMediaIDKey] = normalized.AvatarMediaID
	}

	data, err := buildProfileMarkdown(frontMatter, normalized.Bio)
	if err != nil {
		return fmt.Errorf("marshalling profile config: %w", err)
	}
	return atomicfile.WriteFileWithMode(profileFile, data, 0600)
}

func parseProfileMarkdown(data []byte) (map[string]string, string, error) {
	content := string(data)
	frontMatter := map[string]string{}

	if !strings.HasPrefix(content, profileFrontMatterDelimiter+"\n") {
		return frontMatter, content, nil
	}

	rest := content[len(profileFrontMatterDelimiter)+1:]
	endMarker := "\n" + profileFrontMatterDelimiter + "\n"
	endIndex := strings.Index(rest, endMarker)
	bodyStartOffset := len(endMarker)
	if endIndex == -1 {
		endMarker = "\n" + profileFrontMatterDelimiter
		endIndex = strings.Index(rest, endMarker)
		bodyStartOffset = len(endMarker)
	}
	if endIndex == -1 {
		return nil, "", fmt.Errorf("missing closing front matter delimiter")
	}

	frontMatterContent := rest[:endIndex]
	body := rest[endIndex+bodyStartOffset:]

	decoded := map[string]interface{}{}
	if strings.TrimSpace(frontMatterContent) != "" {
		if err := yaml.Unmarshal([]byte(frontMatterContent), &decoded); err != nil {
			return nil, "", fmt.Errorf("invalid YAML front matter: %w", err)
		}
	}
	for key, value := range decoded {
		frontMatter[key] = strings.TrimSpace(fmt.Sprint(value))
	}

	return frontMatter, body, nil
}

func buildProfileMarkdown(frontMatter map[string]string, body string) ([]byte, error) {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	keys := make([]string, 0, len(frontMatter))
	for key := range frontMatter {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// Keep required keys near the top for readability.
	sort.SliceStable(keys, func(i, j int) bool {
		left := keys[i]
		right := keys[j]
		if left == profileNameKey {
			return true
		}
		if right == profileNameKey {
			return false
		}
		if left == profileAvatarMediaIDKey {
			return right != profileNameKey
		}
		if right == profileAvatarMediaIDKey {
			return false
		}
		return left < right
	})

	for _, key := range keys {
		value := frontMatter[key]
		node.Content = append(
			node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
		)
	}

	var frontMatterBuffer bytes.Buffer
	encoder := yaml.NewEncoder(&frontMatterBuffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}

	var output bytes.Buffer
	output.WriteString(profileFrontMatterDelimiter)
	output.WriteByte('\n')
	output.Write(frontMatterBuffer.Bytes())
	output.WriteString(profileFrontMatterDelimiter)
	output.WriteByte('\n')
	output.WriteString(body)
	return output.Bytes(), nil
}
