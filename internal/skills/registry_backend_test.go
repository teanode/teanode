package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSafePathSegment(t *testing.T) {
	tests := []struct {
		value string
		safe  bool
	}{
		{value: "git", safe: true},
		{value: "1.0.0", safe: true},
		{value: "", safe: false},
		{value: ".", safe: false},
		{value: "..", safe: false},
		{value: "../x", safe: false},
		{value: "a/b", safe: false},
		{value: `a\b`, safe: false},
	}
	for _, testCase := range tests {
		if got := isSafePathSegment(testCase.value); got != testCase.safe {
			t.Fatalf("isSafePathSegment(%q) = %v, want %v", testCase.value, got, testCase.safe)
		}
	}
}

func TestResolveInstallDirRejectsTraversal(t *testing.T) {
	if _, err := resolveInstallDir("/tmp/skills", "../escape", "1.0.0"); err == nil {
		t.Fatal("expected invalid skill name error")
	}
	if _, err := resolveInstallDir("/tmp/skills", "git", "../../escape"); err == nil {
		t.Fatal("expected invalid skill version error")
	}
}

func TestEnsureNoSymlinkComponentsRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "git")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if err := ensureNoSymlinkComponents(root, filepath.Join(link, "1.0.0")); err == nil {
		t.Fatal("expected symlink component rejection")
	}
}
