package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempConfigDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	SetDirectory(directory)
	t.Cleanup(func() { SetDirectory("") })
	return directory
}

func TestLoadProfile_MissingFileFallsBackToOSUsername(t *testing.T) {
	t.Parallel()

	withTempConfigDirectory(t)

	profile, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if profile.Name != OSUsername() {
		t.Fatalf("profile name = %q, want %q", profile.Name, OSUsername())
	}
}

func TestLoadProfile_EmptyNameFallsBackToOSUsername(t *testing.T) {
	t.Parallel()

	directory := withTempConfigDirectory(t)
	content := strings.Join([]string{
		"---",
		"name: \"   \"",
		"avatarMediaId: media_123",
		"---",
		"test",
	}, "\n")
	if err := os.WriteFile(filepath.Join(directory, "profile.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write profile file: %v", err)
	}

	profile, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if profile.Name != OSUsername() {
		t.Fatalf("profile name = %q, want %q", profile.Name, OSUsername())
	}
	if profile.Bio != "test" {
		t.Fatalf("profile bio = %q, want test", profile.Bio)
	}
	if profile.AvatarMediaID != "media_123" {
		t.Fatalf("avatarMediaId = %q, want %q", profile.AvatarMediaID, "media_123")
	}
}

func TestSaveProfileAndLoadProfile(t *testing.T) {
	t.Parallel()

	withTempConfigDirectory(t)
	input := &Profile{
		Name:          "Alice",
		Bio:           "Builder",
		AvatarMediaID: "media_123",
	}
	if err := SaveProfile(input); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	got, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if got.Name != input.Name {
		t.Fatalf("name = %q, want %q", got.Name, input.Name)
	}
	if got.Bio != input.Bio {
		t.Fatalf("bio = %q, want %q", got.Bio, input.Bio)
	}
	if got.AvatarMediaID != input.AvatarMediaID {
		t.Fatalf("avatarMediaId = %q, want %q", got.AvatarMediaID, input.AvatarMediaID)
	}

	profileFile, err := ProfileFile()
	if err != nil {
		t.Fatalf("ProfileFile failed: %v", err)
	}
	data, err := os.ReadFile(profileFile)
	if err != nil {
		t.Fatalf("failed to read profile file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "---\n") {
		t.Fatalf("profile file missing front matter delimiters: %q", text)
	}
	if !strings.Contains(text, "name: Alice\n") {
		t.Fatalf("profile file missing name field: %q", text)
	}
	if !strings.Contains(text, "avatarMediaId: media_123\n") {
		t.Fatalf("profile file missing avatarMediaId field: %q", text)
	}
}

func TestSaveProfile_EmptyNameFallsBackToOSUsername(t *testing.T) {
	t.Parallel()

	withTempConfigDirectory(t)
	input := &Profile{
		Name: "   ",
		Bio:  "Bio",
	}
	if err := SaveProfile(input); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	got, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if got.Name != OSUsername() {
		t.Fatalf("name = %q, want %q", got.Name, OSUsername())
	}
}

func TestLoadProfile_DoesNotReadLegacyProfileYAML(t *testing.T) {
	t.Parallel()

	directory := withTempConfigDirectory(t)
	data := []byte("name: Legacy Name\nbio: legacy bio\navatarMediaId: legacy_media\n")
	if err := os.WriteFile(filepath.Join(directory, "profile.yaml"), data, 0644); err != nil {
		t.Fatalf("failed to write profile file: %v", err)
	}

	profile, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if profile.Name != OSUsername() {
		t.Fatalf("profile name = %q, want %q", profile.Name, OSUsername())
	}
	if profile.Bio != "" {
		t.Fatalf("profile bio = %q, want empty", profile.Bio)
	}
	if profile.AvatarMediaID != "" {
		t.Fatalf("avatarMediaId = %q, want empty", profile.AvatarMediaID)
	}
}

func TestSaveProfile_PreservesUnknownFrontMatterKeys(t *testing.T) {
	t.Parallel()

	directory := withTempConfigDirectory(t)
	content := strings.Join([]string{
		"---",
		"name: Alice",
		"timezone: UTC",
		"favorite: tea",
		"---",
		"hello",
	}, "\n")
	if err := os.WriteFile(filepath.Join(directory, "profile.md"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write profile file: %v", err)
	}

	if err := SaveProfile(&Profile{Name: "Bob", AvatarMediaID: "media_9", Bio: "bio"}); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "profile.md"))
	if err != nil {
		t.Fatalf("failed to read profile file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "timezone: UTC\n") {
		t.Fatalf("profile file missing preserved key timezone: %q", text)
	}
	if !strings.Contains(text, "favorite: tea\n") {
		t.Fatalf("profile file missing preserved key favorite: %q", text)
	}
}

func TestSaveProfile_PreservesExistingBioByDefault(t *testing.T) {
	t.Parallel()

	withTempConfigDirectory(t)
	if err := SaveProfileOverwriteBio(&Profile{Name: "Alice", Bio: "# Existing"}); err != nil {
		t.Fatalf("SaveProfileOverwriteBio failed: %v", err)
	}

	if err := SaveProfile(&Profile{Name: "Alice Updated"}); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	got, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if got.Name != "Alice Updated" {
		t.Fatalf("name = %q, want %q", got.Name, "Alice Updated")
	}
	if got.Bio != "# Existing" {
		t.Fatalf("bio = %q, want %q", got.Bio, "# Existing")
	}
}

func TestSaveProfileOverwriteBio_ClearsBio(t *testing.T) {
	t.Parallel()

	withTempConfigDirectory(t)
	if err := SaveProfileOverwriteBio(&Profile{Name: "Alice", Bio: "# Existing"}); err != nil {
		t.Fatalf("SaveProfileOverwriteBio failed: %v", err)
	}

	if err := SaveProfileOverwriteBio(&Profile{Name: "Alice", Bio: ""}); err != nil {
		t.Fatalf("SaveProfileOverwriteBio failed: %v", err)
	}

	got, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if got.Bio != "" {
		t.Fatalf("bio = %q, want empty", got.Bio)
	}
}

func TestSaveProfile_Writes0600Permissions(t *testing.T) {
	t.Parallel()

	directory := withTempConfigDirectory(t)
	if err := SaveProfile(&Profile{Name: "Alice", Bio: "# Bio\n- bullet"}); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(directory, "profile.md"))
	if err != nil {
		t.Fatalf("failed to stat profile file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("profile permissions = %o, want %o", info.Mode().Perm(), 0600)
	}
}

func TestLoadProfile_MarkdownBioRoundTrip(t *testing.T) {
	t.Parallel()

	withTempConfigDirectory(t)
	input := &Profile{
		Name: "Alice",
		Bio: strings.Join([]string{
			"# About",
			"",
			"- likes tea",
			"- writes markdown",
		}, "\n"),
	}
	if err := SaveProfile(input); err != nil {
		t.Fatalf("SaveProfile failed: %v", err)
	}

	got, err := LoadProfile()
	if err != nil {
		t.Fatalf("LoadProfile failed: %v", err)
	}
	if got.Bio != input.Bio {
		t.Fatalf("bio = %q, want %q", got.Bio, input.Bio)
	}
}
