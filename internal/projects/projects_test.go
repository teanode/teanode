package projects

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teanode/teanode/internal/configs"
)

func withTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	configs.SetDirectory(directory)
	t.Cleanup(func() { configs.SetDirectory("") })
	return directory
}

func TestCreateListAndProjectFile(t *testing.T) {
	withTempDir(t)

	metadata, err := CreateProject("Alpha", "", "Build an internal tool")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if metadata.ID == "" {
		t.Fatal("project ID should not be empty")
	}
	if metadata.Name != "Alpha" {
		t.Fatalf("name = %q, want Alpha", metadata.Name)
	}
	list, err := configs.LoadProjectConfigs()
	if err != nil {
		t.Fatalf("LoadProjectConfigs() error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].ID != metadata.ID {
		t.Fatalf("list[0].ID = %q, want %q", list[0].ID, metadata.ID)
	}

	directory := configs.ProjectsDirectory()
	projectFile := filepath.Join(directory, metadata.ID, defaultProjectDocumentName)
	content, err := readFile(metadata.ID, defaultProjectDocumentName)
	if err != nil {
		t.Fatalf("readFile(PROJECT.md) error: %v", err)
	}
	if content == "" {
		t.Fatalf("PROJECT.md at %s should not be empty", projectFile)
	}
}

func TestTouchBumpsUpdatedOnWrites(t *testing.T) {
	withTempDir(t)

	metadata, err := CreateProject("Beta", "Test metadata updates", "")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	before := metadata.UpdatedAt
	time.Sleep(2 * time.Millisecond)

	if err := writeFile(metadata.ID, "notes.md", "hello"); err != nil {
		t.Fatalf("writeFile() error: %v", err)
	}
	afterWrite, err := configs.LoadProjectConfig(metadata.ID)
	if err != nil {
		t.Fatalf("LoadProjectConfig() error after write: %v", err)
	}
	if !afterWrite.UpdatedAt.Time.After(before.Time) {
		t.Fatalf("updatedAt after write = %s, want > %s", afterWrite.UpdatedAt.String(), before.String())
	}

	time.Sleep(2 * time.Millisecond)
	if err := appendFile(metadata.ID, "notes.md", "world"); err != nil {
		t.Fatalf("appendFile() error: %v", err)
	}
	afterAppend, err := configs.LoadProjectConfig(metadata.ID)
	if err != nil {
		t.Fatalf("LoadProjectConfig() error after append: %v", err)
	}
	if !afterAppend.UpdatedAt.Time.After(afterWrite.UpdatedAt.Time) {
		t.Fatalf("updatedAt after append = %s, want > %s", afterAppend.UpdatedAt.String(), afterWrite.UpdatedAt.String())
	}

	time.Sleep(2 * time.Millisecond)
	if err := moveFile(metadata.ID, "notes.md", "archive/notes.md"); err != nil {
		t.Fatalf("moveFile() error: %v", err)
	}
	afterMove, err := configs.LoadProjectConfig(metadata.ID)
	if err != nil {
		t.Fatalf("LoadProjectConfig() error after move: %v", err)
	}
	if !afterMove.UpdatedAt.Time.After(afterAppend.UpdatedAt.Time) {
		t.Fatalf("updatedAt after move = %s, want > %s", afterMove.UpdatedAt.String(), afterAppend.UpdatedAt.String())
	}

	time.Sleep(2 * time.Millisecond)
	if err := deleteFile(metadata.ID, "archive/notes.md"); err != nil {
		t.Fatalf("deleteFile() error: %v", err)
	}
	afterDelete, err := configs.LoadProjectConfig(metadata.ID)
	if err != nil {
		t.Fatalf("LoadProjectConfig() error after delete: %v", err)
	}
	if !afterDelete.UpdatedAt.Time.After(afterMove.UpdatedAt.Time) {
		t.Fatalf("updatedAt after delete = %s, want > %s", afterDelete.UpdatedAt.String(), afterMove.UpdatedAt.String())
	}
}

func TestRenameAndDelete(t *testing.T) {
	withTempDir(t)
	metadata, err := CreateProject("Gamma", "", "")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	renamed, err := RenameProject(metadata.ID, "Gamma Prime")
	if err != nil {
		t.Fatalf("RenameProject() error: %v", err)
	}
	if renamed.Name != "Gamma Prime" {
		t.Fatalf("renamed.Name = %q, want Gamma Prime", renamed.Name)
	}

	if err := DeleteProject(metadata.ID); err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	}
	if _, err := configs.LoadProjectConfig(metadata.ID); err == nil {
		t.Fatal("LoadProjectConfig() should fail after delete")
	}
}

func TestReadFileRejectsSymlinkComponents(t *testing.T) {
	withTempDir(t)

	metadata, err := CreateProject("Delta", "", "")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	workspace, err := WorkspaceDirectory(metadata.ID)
	if err != nil {
		t.Fatalf("WorkspaceDirectory() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "docs"), 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	target := filepath.Join(workspace, "real.md")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatalf("writeFile() setup error: %v", err)
	}

	linkPath := filepath.Join(workspace, "docs", "link.md")
	if err := os.Symlink(target, linkPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink not supported: %v", err)
		}
		t.Fatalf("Symlink() error: %v", err)
	}

	if _, err := readFile(metadata.ID, "docs/link.md"); err == nil {
		t.Fatal("ReadFile should reject symlink components")
	}
}

func TestFileOperationsDenyPathTraversal(t *testing.T) {
	withTempDir(t)

	metadata, err := CreateProject("Epsilon", "", "")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	if err := writeFile(metadata.ID, "../escape.txt", "nope"); err == nil {
		t.Fatal("WriteFile should reject path traversal")
	}
	if _, err := readFile(metadata.ID, "../escape.txt"); err == nil {
		t.Fatal("ReadFile should reject path traversal")
	}
}

func TestWriteFileRejectsSymlinkDirectoryComponents(t *testing.T) {
	withTempDir(t)

	metadata, err := CreateProject("Zeta", "", "")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	workspace, err := WorkspaceDirectory(metadata.ID)
	if err != nil {
		t.Fatalf("WorkspaceDirectory() error: %v", err)
	}

	targetDirectory := filepath.Join(workspace, "target")
	if err := os.MkdirAll(targetDirectory, 0755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	if err := os.Symlink(targetDirectory, filepath.Join(workspace, "docs")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink not supported: %v", err)
		}
		t.Fatalf("Symlink() error: %v", err)
	}

	if err := writeFile(metadata.ID, "docs/note.md", "hello"); err == nil {
		t.Fatal("WriteFile should reject symlink components")
	}
}

func TestProjectYamlDoesNotPersistIDField(t *testing.T) {
	withTempDir(t)

	metadata, err := CreateProject("Theta", "No ID field in yaml", "")
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	path, err := projectConfigPath(metadata.ID)
	if err != nil {
		t.Fatalf("projectConfigPath() error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readFile(project.yaml) error: %v", err)
	}
	if strings.Contains(string(data), "\nid:") || strings.HasPrefix(string(data), "id:") {
		t.Fatalf("project.yaml should not contain id field, got:\n%s", string(data))
	}
}
