package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommit(t *testing.T) {
	t.Parallel()

	tempDirectory := t.TempDir()

	filename := filepath.Join(tempDirectory, "atomicfile.txt")
	file, err := Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := Commit(file); err != nil {
		t.Fatalf("failed to commit file: %s", err)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}

func TestCommitAs(t *testing.T) {
	t.Parallel()

	tempDirectory := t.TempDir()

	filename := filepath.Join(tempDirectory, "atomicfile.txt")
	filename2 := filepath.Join(tempDirectory, "atomicfile2.txt")
	file, err := Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}
	if _, err := os.Stat(filename2); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := CommitAs(file, filename2); err != nil {
		t.Fatalf("failed to commit file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file with old name exists before commit: %s", err)
	}
	if _, err := os.Stat(filename2); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}

func TestDiscard(t *testing.T) {
	t.Parallel()

	tempDirectory := t.TempDir()

	filename := filepath.Join(tempDirectory, "atomicfile.txt")
	file, err := Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := Discard(file); err != nil {
		t.Fatalf("failed to discard file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists after discard: %s", err)
	}
}

func TestCommitAfterClose(t *testing.T) {
	t.Parallel()

	tempDirectory := t.TempDir()

	filename := filepath.Join(tempDirectory, "atomicfile.txt")
	file, err := Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("failed to close file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := Commit(file); err != nil {
		t.Fatalf("failed to commit file: %s", err)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()

	tempDirectory := t.TempDir()

	filename := filepath.Join(tempDirectory, "atomicfile.txt")
	if err := WriteFile(filename, []byte("test\n")); err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}
