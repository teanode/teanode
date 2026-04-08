//go:build !windows

package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformApplySameDevice(t *testing.T) {
	directory := t.TempDir()

	// Create a fake "current" binary.
	currentPath := filepath.Join(directory, "teanode")
	if err := os.WriteFile(currentPath, []byte("old-binary-content"), 0755); err != nil {
		t.Fatal(err)
	}

	// Create a staged binary in the same directory (same device).
	stagedPath := filepath.Join(directory, "teanode-staged")
	stagedContent := []byte("new-binary-content")
	if err := os.WriteFile(stagedPath, stagedContent, 0755); err != nil {
		t.Fatal(err)
	}

	if err := platformApply(currentPath, stagedPath); err != nil {
		t.Fatalf("platformApply: %v", err)
	}

	// The current path should now contain the new content.
	got, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("reading updated binary: %v", err)
	}
	if string(got) != string(stagedContent) {
		t.Errorf("binary content = %q, want %q", got, stagedContent)
	}

	// The .bak file should have been cleaned up.
	if _, err := os.Stat(currentPath + ".bak"); !os.IsNotExist(err) {
		t.Error("backup file was not cleaned up")
	}

	// The staged file should no longer exist (it was renamed).
	if _, err := os.Stat(stagedPath); !os.IsNotExist(err) {
		t.Error("staged file still exists after same-device rename")
	}
}

func TestPlatformApplyPreservesPermissions(t *testing.T) {
	directory := t.TempDir()

	currentPath := filepath.Join(directory, "teanode")
	if err := os.WriteFile(currentPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	stagedPath := filepath.Join(directory, "teanode-staged")
	if err := os.WriteFile(stagedPath, []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := platformApply(currentPath, stagedPath); err != nil {
		t.Fatalf("platformApply: %v", err)
	}

	info, err := os.Stat(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	// On same-device rename the permissions come from the source file.
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("binary lost executable bit: mode=%o", info.Mode().Perm())
	}
}

func TestPlatformApplyRestoresBackupOnFailure(t *testing.T) {
	directory := t.TempDir()

	currentPath := filepath.Join(directory, "teanode")
	originalContent := []byte("original-binary")
	if err := os.WriteFile(currentPath, originalContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Use a staged path that does not exist — this will cause
	// renameOrCopy to fail, triggering the backup restore path.
	nonExistentStaged := filepath.Join(directory, "does-not-exist")

	err := platformApply(currentPath, nonExistentStaged)
	if err == nil {
		t.Fatal("expected error for non-existent staged file")
	}

	// The original binary should be restored.
	got, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("reading restored binary: %v", err)
	}
	if string(got) != string(originalContent) {
		t.Errorf("restored content = %q, want %q", got, originalContent)
	}
}

func TestCopyFileCrossDevice(t *testing.T) {
	// Simulate the cross-device path by using copyFile directly with
	// source and destination in the same temp directory.
	directory := t.TempDir()

	sourcePath := filepath.Join(directory, "source")
	sourceContent := []byte("binary-content-for-copy")
	if err := os.WriteFile(sourcePath, sourceContent, 0755); err != nil {
		t.Fatal(err)
	}

	destinationPath := filepath.Join(directory, "destination")

	if err := copyFile(sourcePath, destinationPath); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	// Verify destination content.
	got, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if string(got) != string(sourceContent) {
		t.Errorf("destination content = %q, want %q", got, sourceContent)
	}

	// Verify permissions were preserved.
	info, err := os.Stat(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("destination lost executable bit: mode=%o", info.Mode().Perm())
	}

	// Source should be removed.
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Error("source file was not removed after copy")
	}
}

func TestCopyFileAtomicity(t *testing.T) {
	// Verify that an incomplete copy does not leave a file at the
	// destination. We simulate this by making the source unreadable
	// after stat but before copy completes — but that's hard to do
	// precisely. Instead, test that a failed copy cleans up the temp file.
	directory := t.TempDir()

	// Source that cannot be opened.
	sourcePath := filepath.Join(directory, "no-such-source")
	destinationPath := filepath.Join(directory, "destination")

	err := copyFile(sourcePath, destinationPath)
	if err == nil {
		t.Fatal("expected error for non-existent source")
	}

	// Destination should not exist.
	if _, err := os.Stat(destinationPath); !os.IsNotExist(err) {
		t.Error("destination should not exist after failed copy")
	}

	// No temp files should be left behind.
	entries, _ := filepath.Glob(filepath.Join(directory, ".teanode-update-*"))
	if len(entries) > 0 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestRenameOrCopySameDevice(t *testing.T) {
	directory := t.TempDir()

	sourcePath := filepath.Join(directory, "source")
	content := []byte("test-content")
	if err := os.WriteFile(sourcePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	destinationPath := filepath.Join(directory, "destination")

	if err := renameOrCopy(sourcePath, destinationPath); err != nil {
		t.Fatalf("renameOrCopy: %v", err)
	}

	got, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content = %q, want %q", got, content)
	}

	// Source should be gone (renamed, not copied).
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Error("source still exists after same-device rename")
	}
}

func TestPlatformApplyCleansStaleBackup(t *testing.T) {
	directory := t.TempDir()

	currentPath := filepath.Join(directory, "teanode")
	if err := os.WriteFile(currentPath, []byte("current"), 0755); err != nil {
		t.Fatal(err)
	}

	// Pre-create a stale .bak from a prior failed update.
	backupPath := currentPath + ".bak"
	if err := os.WriteFile(backupPath, []byte("stale-backup"), 0755); err != nil {
		t.Fatal(err)
	}

	stagedPath := filepath.Join(directory, "staged")
	if err := os.WriteFile(stagedPath, []byte("new-version"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := platformApply(currentPath, stagedPath); err != nil {
		t.Fatalf("platformApply with stale backup: %v", err)
	}

	got, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-version" {
		t.Errorf("content = %q, want %q", got, "new-version")
	}

	// The stale .bak should be gone.
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Error(".bak not cleaned up")
	}
}

func TestCheckDirectoryWritable(t *testing.T) {
	directory := t.TempDir()
	if err := checkDirectoryWritable(directory); err != nil {
		t.Fatalf("writable directory: %v", err)
	}

	// Non-existent directory.
	if err := checkDirectoryWritable(filepath.Join(directory, "nope")); err == nil {
		t.Error("expected error for non-existent directory")
	}

	// File instead of directory.
	filePath := filepath.Join(directory, "file")
	_ = os.WriteFile(filePath, nil, 0644)
	if err := checkDirectoryWritable(filePath); err == nil {
		t.Error("expected error when path is a file")
	}
}
