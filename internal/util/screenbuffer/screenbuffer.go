// Package screenbuffer provides a line-based terminal screen buffer that
// strips ANSI escape sequences and handles carriage returns at write time.
// This prevents progress bar updates and other line-overwriting output from
// flooding the buffer with redundant content.
package screenbuffer

import (
	"strings"
	"sync"
)

// Buffer maintains a line-based terminal screen buffer.
type Buffer struct {
	mutex        sync.Mutex
	lines        []string // completed lines
	maxLines     int      // maximum retained lines
	currentLine  []byte   // line currently being written
	cursorColumn int      // write position in currentLine
	escapeState  int      // 0=normal, 1=escape, 2=csi, 3=osc
}

// New creates a new screen buffer that retains at most maxLines completed lines.
func New(maxLines int) *Buffer {
	return &Buffer{
		maxLines: maxLines,
	}
}

// Write processes terminal output, stripping ANSI escape sequences and
// handling carriage returns, newlines, backspace, and tab characters.
func (self *Buffer) Write(input []byte) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, byteValue := range input {
		switch self.escapeState {
		case 1: // received ESC
			switch byteValue {
			case '[':
				self.escapeState = 2 // CSI sequence
			case ']':
				self.escapeState = 3 // OSC sequence
			default:
				self.escapeState = 0 // single-char escape, done
			}

		case 2: // CSI sequence: wait for final byte (0x40-0x7E)
			if byteValue >= 0x40 && byteValue <= 0x7E {
				self.escapeState = 0
			}

		case 3: // OSC sequence: wait for BEL (0x07)
			if byteValue == 0x07 {
				self.escapeState = 0
			}

		default: // normal character processing
			switch {
			case byteValue == 0x1b:
				self.escapeState = 1

			case byteValue == '\n':
				self.lines = append(self.lines, string(self.currentLine))
				if len(self.lines) > self.maxLines {
					copy(self.lines, self.lines[1:])
					self.lines = self.lines[:self.maxLines]
				}
				self.currentLine = self.currentLine[:0]
				self.cursorColumn = 0

			case byteValue == '\r':
				self.cursorColumn = 0

			case byteValue == '\b':
				if self.cursorColumn > 0 {
					self.cursorColumn--
				}

			case byteValue == '\t':
				// Advance to next tab stop (every 8 columns).
				nextStop := ((self.cursorColumn / 8) + 1) * 8
				for self.cursorColumn < nextStop {
					if self.cursorColumn < len(self.currentLine) {
						self.currentLine[self.cursorColumn] = ' '
					} else {
						self.currentLine = append(self.currentLine, ' ')
					}
					self.cursorColumn++
				}

			case byteValue >= 32: // printable characters (including UTF-8 bytes >= 128)
				if self.cursorColumn < len(self.currentLine) {
					self.currentLine[self.cursorColumn] = byteValue
				} else {
					for len(self.currentLine) < self.cursorColumn {
						self.currentLine = append(self.currentLine, ' ')
					}
					self.currentLine = append(self.currentLine, byteValue)
				}
				self.cursorColumn++
			}
			// Other control characters (< 32) are silently ignored.
		}
	}
}

// Screenshot returns a snapshot of the most recent lines (up to maxLines)
// including the current partial line, with trailing blank lines trimmed.
func (self *Buffer) Screenshot(maxLines int) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	// Collect lines for the screenshot.
	var result []string
	startIndex := 0
	if len(self.lines) > maxLines-1 {
		startIndex = len(self.lines) - (maxLines - 1)
	}
	result = append(result, self.lines[startIndex:]...)
	if len(self.currentLine) > 0 {
		result = append(result, string(self.currentLine))
	}

	// Trim trailing empty lines.
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	return strings.Join(result, "\n")
}
