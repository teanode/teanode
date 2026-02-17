package screenbuffer

import (
	"testing"
)

func TestNewlines(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("line1\nline2\nline3\n"))

	got := buf.Screenshot(100)
	want := "line1\nline2\nline3"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCurrentPartialLine(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("line1\npartial"))

	got := buf.Screenshot(100)
	want := "line1\npartial"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCarriageReturnOverwrite(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("old text\rnew"))

	got := buf.Screenshot(100)
	want := "new text"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestProgressBarOverwrite(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("Progress: 50%\rProgress: 100%\n"))

	got := buf.Screenshot(100)
	want := "Progress: 100%"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestMaxLinesRetention(t *testing.T) {
	t.Parallel()

	buf := New(3)
	buf.Write([]byte("a\nb\nc\nd\ne\n"))

	got := buf.Screenshot(100)
	// Only the last 3 completed lines should be retained.
	want := "c\nd\ne"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestScreenshotMaxLines(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("a\nb\nc\nd\ne\n"))

	// Screenshot(n) reserves 1 slot for the current partial line,
	// so it shows n-1 completed lines plus the partial line.
	got := buf.Screenshot(3)
	want := "d\ne"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestScreenshotMaxLinesWithPartial(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("a\nb\nc\nd\npartial"))

	got := buf.Screenshot(3)
	// 2 completed lines + partial line.
	want := "c\nd\npartial"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestScreenshotTrimsTrailingBlanks(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("hello\n\n\n"))

	got := buf.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestStripCSIEscape(t *testing.T) {
	t.Parallel()

	buf := New(100)
	// ESC[31m = red, ESC[0m = reset
	buf.Write([]byte("\x1b[31mred text\x1b[0m\n"))

	got := buf.Screenshot(100)
	want := "red text"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestStripOSCEscape(t *testing.T) {
	t.Parallel()

	buf := New(100)
	// OSC sequence: ESC ] ... BEL
	buf.Write([]byte("\x1b]0;window title\x07visible\n"))

	got := buf.Screenshot(100)
	want := "visible"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSingleCharEscape(t *testing.T) {
	t.Parallel()

	buf := New(100)
	// ESC followed by a non-[ non-] character: single-char escape, consumed.
	buf.Write([]byte("\x1bMhello\n"))

	got := buf.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBackspace(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("abc\b\bXY"))

	got := buf.Screenshot(100)
	want := "aXY"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBackspaceAtStart(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("\b\bhello"))

	got := buf.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTab(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("a\tb\n"))

	got := buf.Screenshot(100)
	// 'a' at col 0, tab goes to col 8, 'b' at col 8.
	want := "a       b"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestControlCharactersIgnored(t *testing.T) {
	t.Parallel()

	buf := New(100)
	// BEL (0x07) and other control chars below 32 (except \n, \r, \b, \t, ESC) should be ignored.
	buf.Write([]byte("he\x01ll\x02o\n"))

	got := buf.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestEmptyBuffer(t *testing.T) {
	t.Parallel()

	buf := New(100)
	got := buf.Screenshot(100)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestIncrementalWrites(t *testing.T) {
	t.Parallel()

	buf := New(100)
	buf.Write([]byte("hel"))
	buf.Write([]byte("lo\nwor"))
	buf.Write([]byte("ld"))

	got := buf.Screenshot(100)
	want := "hello\nworld"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
