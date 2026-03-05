package screenbuffer

import (
	"testing"
)

func TestNewlines(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("line1\nline2\nline3\n"))

	got := buffer.Screenshot(100)
	want := "line1\nline2\nline3"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCurrentPartialLine(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("line1\npartial"))

	got := buffer.Screenshot(100)
	want := "line1\npartial"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCarriageReturnOverwrite(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("old text\rnew"))

	got := buffer.Screenshot(100)
	want := "new text"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestProgressBarOverwrite(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("Progress: 50%\rProgress: 100%\n"))

	got := buffer.Screenshot(100)
	want := "Progress: 100%"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestMaxLinesRetention(t *testing.T) {
	t.Parallel()

	buffer := New(3)
	buffer.Write([]byte("a\nb\nc\nd\ne\n"))

	got := buffer.Screenshot(100)
	// Only the last 3 completed lines should be retained.
	want := "c\nd\ne"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestScreenshotMaxLines(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("a\nb\nc\nd\ne\n"))

	got := buffer.Screenshot(3)
	want := "c\nd\ne"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestScreenshotMaxLinesWithPartial(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("a\nb\nc\nd\npartial"))

	got := buffer.Screenshot(3)
	// 2 completed lines + partial line.
	want := "c\nd\npartial"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestScreenshotTrimsTrailingBlanks(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("hello\n\n\n"))

	got := buffer.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestStripCSIEscape(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	// ESC[31m = red, ESC[0m = reset
	buffer.Write([]byte("\x1b[31mred text\x1b[0m\n"))

	got := buffer.Screenshot(100)
	want := "red text"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestStripOSCEscape(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	// OSC sequence: ESC ] ... BEL
	buffer.Write([]byte("\x1b]0;window title\x07visible\n"))

	got := buffer.Screenshot(100)
	want := "visible"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSingleCharEscape(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	// ESC followed by a non-[ non-] character: single-char escape, consumed.
	buffer.Write([]byte("\x1bMhello\n"))

	got := buffer.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBackspace(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("abc\b\bXY"))

	got := buffer.Screenshot(100)
	want := "aXY"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBackspaceAtStart(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("\b\bhello"))

	got := buffer.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestTab(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("a\tb\n"))

	got := buffer.Screenshot(100)
	// 'a' at col 0, tab goes to col 8, 'b' at col 8.
	want := "a       b"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestControlCharactersIgnored(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	// BEL (0x07) and other control chars below 32 (except \n, \r, \b, \t, ESC) should be ignored.
	buffer.Write([]byte("he\x01ll\x02o\n"))

	got := buffer.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestEmptyBuffer(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	got := buffer.Screenshot(100)
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestIncrementalWrites(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("hel"))
	buffer.Write([]byte("lo\nwor"))
	buffer.Write([]byte("ld"))

	got := buffer.Screenshot(100)
	want := "hello\nworld"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestStripCharsetDesignationEscape(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	// ESC(B and ESC)0 are common in ncurses full-screen redraws.
	buffer.Write([]byte("\x1b(B\x1b)0hello\n"))

	got := buffer.Screenshot(100)
	want := "hello"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestStripOSCWithSTTerminator(t *testing.T) {
	t.Parallel()

	buffer := New(100)
	buffer.Write([]byte("\x1b]0;title\x1b\\visible\n"))

	got := buffer.Screenshot(100)
	want := "visible"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestCSIAbsoluteCursorPosition(t *testing.T) {
	t.Parallel()

	buffer := NewWithSize(100, 4, 20)
	// Clear screen, write header, then move to row 3 col 5 and write "CPU".
	buffer.Write([]byte("\x1b[2Jheader\x1b[3;5HCPU"))

	got := buffer.Screenshot(100)
	want := "header\n\n    CPU"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAltScreenSwitch(t *testing.T) {
	t.Parallel()

	buffer := NewWithSize(100, 4, 20)
	buffer.Write([]byte("shell\n"))
	buffer.Write([]byte("\x1b[?1049hhtop"))
	if got := buffer.Screenshot(100); got != "htop" {
		t.Fatalf("expected alt screen content %q, got %q", "htop", got)
	}

	buffer.Write([]byte("\x1b[?1049l"))
	got := buffer.Screenshot(100)
	want := "shell"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResizeAfterAltScreenDoesNotPanic(t *testing.T) {
	t.Parallel()

	buffer := NewWithSize(100, 61, 104)
	buffer.Write([]byte("shell\n"))
	buffer.Write([]byte("\x1b[?1049h"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer.Resize(61, 211)
		buffer.Resize(30, 100)
		buffer.Resize(61, 104)
	}()
	<-done
}
