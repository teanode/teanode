package version

import (
	"testing"
)

func TestVersionReturnsDefault(t *testing.T) {
	t.Parallel()
	if Version() != "0.1.0" {
		t.Fatalf("expected default version \"0.1.0\", got %q", Version())
	}
}

func TestCommitReturnsDefault(t *testing.T) {
	t.Parallel()
	if Commit() != "unknown" {
		t.Fatalf("expected default commit \"unknown\", got %q", Commit())
	}
}

func TestServerNameFormat(t *testing.T) {
	t.Parallel()
	expected := "TeaNode/0.1.0+unknown"
	if ServerName() != expected {
		t.Fatalf("expected %q, got %q", expected, ServerName())
	}
}

func TestServerNameReflectsOverrides(t *testing.T) {
	originalVersion := version
	originalCommit := commit
	t.Cleanup(func() {
		version = originalVersion
		commit = originalCommit
	})

	version = "2.3.4"
	commit = "abc1234"

	if Version() != "2.3.4" {
		t.Fatalf("expected version \"2.3.4\", got %q", Version())
	}
	if Commit() != "abc1234" {
		t.Fatalf("expected commit \"abc1234\", got %q", Commit())
	}
	expected := "TeaNode/2.3.4+abc1234"
	if ServerName() != expected {
		t.Fatalf("expected %q, got %q", expected, ServerName())
	}
}
