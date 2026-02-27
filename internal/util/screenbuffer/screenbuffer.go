// Package screenbuffer provides a terminal screen model that consumes PTY bytes
// and produces plain-text screenshots.
package screenbuffer

import (
	"strconv"
	"strings"
	"sync"
)

const (
	defaultRows = 24
	defaultCols = 80
)

const (
	stateNormal = iota
	stateEsc
	stateCSI
	stateOSC
	stateEscString
	stateEscCharset
)

// Buffer maintains a text screen model and scrollback from terminal output.
type Buffer struct {
	mutex sync.Mutex

	maxLines int

	rows int
	cols int

	screen    [][]rune
	cursorRow int
	cursorCol int
	savedRow  int
	savedCol  int

	scrollback []string

	altScreen         bool
	primaryScreen     [][]rune
	primaryCursorRow  int
	primaryCursorCol  int
	primarySavedRow   int
	primarySavedCol   int
	primaryScrollback []string

	state  int
	csiBuffer []byte
	oscEsc bool
}

// New creates a new screen buffer with a default 24x80 viewport.
func New(maxLines int) *Buffer {
	return NewWithSize(maxLines, defaultRows, defaultCols)
}

// NewWithSize creates a new screen buffer with a specific viewport size.
func NewWithSize(maxLines int, rows, cols uint16) *Buffer {
	sanitizedRows, sanitizedCols := sanitizeSize(int(rows), int(cols))
	buffer := &Buffer{
		maxLines: maxLines,
		rows:     sanitizedRows,
		cols:     sanitizedCols,
	}
	buffer.screen = makeScreen(sanitizedRows, sanitizedCols)
	return buffer
}

// Resize resizes the virtual screen while preserving top-left content.
func (self *Buffer) Resize(rows, cols uint16) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	sanitizedRows, sanitizedCols := sanitizeSize(int(rows), int(cols))
	if sanitizedRows == self.rows && sanitizedCols == self.cols {
		return
	}

	oldRows, oldCols := self.rows, self.cols
	self.screen = resizeScreen(self.screen, oldRows, oldCols, sanitizedRows, sanitizedCols)
	if self.primaryScreen != nil {
		self.primaryScreen = resizeScreen(self.primaryScreen, oldRows, oldCols, sanitizedRows, sanitizedCols)
	}

	self.rows = sanitizedRows
	self.cols = sanitizedCols
	if self.cursorRow >= self.rows {
		self.cursorRow = self.rows - 1
	}
	if self.cursorCol >= self.cols {
		self.cursorCol = self.cols - 1
	}
	if self.savedRow >= self.rows {
		self.savedRow = self.rows - 1
	}
	if self.savedCol >= self.cols {
		self.savedCol = self.cols - 1
	}

	if self.primaryScreen != nil {
		if self.primaryCursorRow >= sanitizedRows {
			self.primaryCursorRow = sanitizedRows - 1
		}
		if self.primaryCursorCol >= sanitizedCols {
			self.primaryCursorCol = sanitizedCols - 1
		}
		if self.primarySavedRow >= sanitizedRows {
			self.primarySavedRow = sanitizedRows - 1
		}
		if self.primarySavedCol >= sanitizedCols {
			self.primarySavedCol = sanitizedCols - 1
		}
	}
}

// Write processes terminal output and updates the virtual screen.
func (self *Buffer) Write(input []byte) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	for _, byteValue := range input {
		switch self.state {
		case stateEsc:
			self.consumeEsc(byteValue)
			continue
		case stateCSI:
			self.consumeCSI(byteValue)
			continue
		case stateOSC:
			self.consumeOSC(byteValue)
			continue
		case stateEscString:
			self.consumeEscString(byteValue)
			continue
		case stateEscCharset:
			self.state = stateNormal
			continue
		}

		switch byteValue {
		case 0x1b:
			self.state = stateEsc
		case 0x9b: // 8-bit CSI
			self.state = stateCSI
			self.csiBuffer = self.csiBuffer[:0]
		case '\n':
			self.lineFeed()
			self.cursorCol = 0
		case '\r':
			self.cursorCol = 0
		case '\b':
			if self.cursorCol > 0 {
				self.cursorCol--
			}
		case '\t':
			nextStop := ((self.cursorCol / 8) + 1) * 8
			for self.cursorCol < nextStop {
				self.putRune(' ')
			}
		case 0x0e, 0x0f:
			// Shift in/out: ignored.
		default:
			if byteValue >= 32 && byteValue != 0x7f {
				self.putRune(rune(byteValue))
			}
		}
	}
}

// Screenshot returns a plain-text snapshot of the most recent content.
func (self *Buffer) Screenshot(maxLines int) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	screenLines := renderScreenLines(self.screen)
	lines := append([]string{}, self.scrollback...)
	lines = append(lines, screenLines...)

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if self.maxLines > 0 && len(lines) > self.maxLines {
		lines = lines[len(lines)-self.maxLines:]
	}

	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return strings.Join(lines, "\n")
}

func (self *Buffer) consumeEsc(byteValue byte) {
	switch byteValue {
	case '[':
		self.state = stateCSI
		self.csiBuffer = self.csiBuffer[:0]
	case ']':
		self.state = stateOSC
		self.oscEsc = false
	case 'P', '^', '_':
		// DCS/PM/APC strings terminated by ST (ESC \)
		self.state = stateEscString
		self.oscEsc = false
	case '(', ')', '*', '+', '-', '.', '/':
		// Character-set designation (e.g. ESC(B, ESC)0).
		self.state = stateEscCharset
	case '7':
		self.savedRow, self.savedCol = self.cursorRow, self.cursorCol
		self.state = stateNormal
	case '8':
		self.cursorRow, self.cursorCol = self.savedRow, self.savedCol
		self.clampCursor()
		self.state = stateNormal
	case 'D':
		self.lineFeed()
		self.state = stateNormal
	case 'E':
		self.cursorCol = 0
		self.lineFeed()
		self.state = stateNormal
	case 'M':
		self.reverseIndex()
		self.state = stateNormal
	case 'c':
		self.reset()
		self.state = stateNormal
	default:
		// Other single-character escapes are ignored.
		self.state = stateNormal
	}
}

func (self *Buffer) consumeCSI(byteValue byte) {
	if byteValue >= 0x40 && byteValue <= 0x7e {
		self.handleCSI(byteValue, string(self.csiBuffer))
		self.csiBuffer = self.csiBuffer[:0]
		self.state = stateNormal
		return
	}
	self.csiBuffer = append(self.csiBuffer, byteValue)
}

func (self *Buffer) consumeOSC(byteValue byte) {
	if byteValue == 0x07 { // BEL terminator
		self.state = stateNormal
		self.oscEsc = false
		return
	}
	if self.oscEsc {
		self.oscEsc = false
		if byteValue == '\\' { // ST terminator (ESC \)
			self.state = stateNormal
		}
		return
	}
	self.oscEsc = (byteValue == 0x1b)
}

func (self *Buffer) consumeEscString(byteValue byte) {
	if self.oscEsc {
		self.oscEsc = false
		if byteValue == '\\' {
			self.state = stateNormal
		}
		return
	}
	self.oscEsc = (byteValue == 0x1b)
}

func (self *Buffer) handleCSI(final byte, raw string) {
	privatePrefix := ""
	if len(raw) > 0 {
		prefix := raw[0]
		if prefix == '?' || prefix == '>' || prefix == '!' || prefix == '=' {
			privatePrefix = string(prefix)
			raw = raw[1:]
		}
	}

	paramsPart := raw
	for index, byteVal := range raw {
		if byteVal >= 0x20 && byteVal <= 0x2f {
			paramsPart = raw[:index]
			break
		}
	}
	params := parseCSIParams(paramsPart)

	switch final {
	case 'A':
		self.cursorRow -= max(1, getParam(params, 0, 1))
	case 'B':
		self.cursorRow += max(1, getParam(params, 0, 1))
	case 'C':
		self.cursorCol += max(1, getParam(params, 0, 1))
	case 'D':
		self.cursorCol -= max(1, getParam(params, 0, 1))
	case 'E':
		self.cursorRow += max(1, getParam(params, 0, 1))
		self.cursorCol = 0
	case 'F':
		self.cursorRow -= max(1, getParam(params, 0, 1))
		self.cursorCol = 0
	case 'G':
		self.cursorCol = max(1, getParam(params, 0, 1)) - 1
	case 'd':
		self.cursorRow = max(1, getParam(params, 0, 1)) - 1
	case 'H', 'f':
		row := max(1, getParam(params, 0, 1))
		col := max(1, getParam(params, 1, 1))
		self.cursorRow = row - 1
		self.cursorCol = col - 1
	case 'J':
		self.eraseDisplay(getParam(params, 0, 0))
	case 'K':
		self.eraseLine(getParam(params, 0, 0))
	case 's':
		self.savedRow, self.savedCol = self.cursorRow, self.cursorCol
	case 'u':
		self.cursorRow, self.cursorCol = self.savedRow, self.savedCol
	case 'h', 'l':
		if privatePrefix == "?" {
			self.handlePrivateMode(final == 'h', params)
		}
	case 'm', 'r', 't', 'n', 'q':
		// Style/status/scroll-region requests ignored for plain-text snapshot.
	}

	self.clampCursor()
}

func (self *Buffer) handlePrivateMode(enable bool, params []int) {
	for _, parameter := range params {
		switch parameter {
		case 47, 1047, 1049:
			if enable {
				self.enterAltScreen()
			} else {
				self.exitAltScreen()
			}
		}
	}
}

func (self *Buffer) enterAltScreen() {
	if self.altScreen {
		return
	}
	self.primaryScreen = cloneScreen(self.screen)
	self.primaryCursorRow = self.cursorRow
	self.primaryCursorCol = self.cursorCol
	self.primarySavedRow = self.savedRow
	self.primarySavedCol = self.savedCol
	self.primaryScrollback = append([]string{}, self.scrollback...)

	self.altScreen = true
	self.screen = makeScreen(self.rows, self.cols)
	self.cursorRow = 0
	self.cursorCol = 0
	self.savedRow = 0
	self.savedCol = 0
	self.scrollback = self.scrollback[:0]
}

func (self *Buffer) exitAltScreen() {
	if !self.altScreen {
		return
	}
	if self.primaryScreen != nil {
		self.screen = self.primaryScreen
	}
	self.cursorRow = self.primaryCursorRow
	self.cursorCol = self.primaryCursorCol
	self.savedRow = self.primarySavedRow
	self.savedCol = self.primarySavedCol
	self.scrollback = append([]string{}, self.primaryScrollback...)

	self.primaryScreen = nil
	self.primaryScrollback = nil
	self.altScreen = false
	self.clampCursor()
}

func (self *Buffer) eraseDisplay(mode int) {
	switch mode {
	case 0:
		self.eraseLine(0)
		for row := self.cursorRow + 1; row < self.rows; row++ {
			for col := 0; col < self.cols; col++ {
				self.screen[row][col] = ' '
			}
		}
	case 1:
		for row := 0; row < self.cursorRow; row++ {
			for col := 0; col < self.cols; col++ {
				self.screen[row][col] = ' '
			}
		}
		for col := 0; col <= self.cursorCol && col < self.cols; col++ {
			self.screen[self.cursorRow][col] = ' '
		}
	default:
		for row := 0; row < self.rows; row++ {
			for col := 0; col < self.cols; col++ {
				self.screen[row][col] = ' '
			}
		}
	}
}

func (self *Buffer) eraseLine(mode int) {
	self.clampCursor()
	switch mode {
	case 0:
		for col := self.cursorCol; col < self.cols; col++ {
			self.screen[self.cursorRow][col] = ' '
		}
	case 1:
		for col := 0; col <= self.cursorCol && col < self.cols; col++ {
			self.screen[self.cursorRow][col] = ' '
		}
	default:
		for col := 0; col < self.cols; col++ {
			self.screen[self.cursorRow][col] = ' '
		}
	}
}

func (self *Buffer) reset() {
	self.screen = makeScreen(self.rows, self.cols)
	self.cursorRow = 0
	self.cursorCol = 0
	self.savedRow = 0
	self.savedCol = 0
	self.scrollback = self.scrollback[:0]
	self.state = stateNormal
	self.csiBuffer = self.csiBuffer[:0]
	self.oscEsc = false
}

func (self *Buffer) reverseIndex() {
	if self.cursorRow > 0 {
		self.cursorRow--
		return
	}
	self.screen = append([][]rune{blankRow(self.cols)}, self.screen...)
	if len(self.screen) > self.rows {
		self.screen = self.screen[:self.rows]
	}
}

func (self *Buffer) lineFeed() {
	if self.cursorRow == self.rows-1 {
		self.appendScrollback(string(trimRightSpaces(self.screen[0])))
		copy(self.screen, self.screen[1:])
		self.screen[self.rows-1] = blankRow(self.cols)
		return
	}
	self.cursorRow++
}

func (self *Buffer) putRune(value rune) {
	self.clampCursor()
	self.screen[self.cursorRow][self.cursorCol] = value
	self.cursorCol++
	if self.cursorCol >= self.cols {
		self.cursorCol = 0
		self.lineFeed()
	}
}

func (self *Buffer) clampCursor() {
	if self.cursorRow < 0 {
		self.cursorRow = 0
	}
	if self.cursorCol < 0 {
		self.cursorCol = 0
	}
	if self.cursorRow >= self.rows {
		self.cursorRow = self.rows - 1
	}
	if self.cursorCol >= self.cols {
		self.cursorCol = self.cols - 1
	}
}

func (self *Buffer) appendScrollback(line string) {
	if self.maxLines <= 0 {
		return
	}
	self.scrollback = append(self.scrollback, line)
	if len(self.scrollback) > self.maxLines {
		self.scrollback = self.scrollback[len(self.scrollback)-self.maxLines:]
	}
}

func parseCSIParams(value string) []int {
	if value == "" {
		return []int{0}
	}
	parts := strings.Split(value, ";")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			out = append(out, 0)
			continue
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			number = 0
		}
		out = append(out, number)
	}
	if len(out) == 0 {
		return []int{0}
	}
	return out
}

func getParam(params []int, index, fallback int) int {
	if index < 0 || index >= len(params) {
		return fallback
	}
	return params[index]
}

func sanitizeSize(rows, cols int) (int, int) {
	if rows <= 0 {
		rows = defaultRows
	}
	if cols <= 0 {
		cols = defaultCols
	}
	return rows, cols
}

func makeScreen(rows, cols int) [][]rune {
	screen := make([][]rune, rows)
	for row := 0; row < rows; row++ {
		screen[row] = blankRow(cols)
	}
	return screen
}

func blankRow(cols int) []rune {
	row := make([]rune, cols)
	for index := range row {
		row[index] = ' '
	}
	return row
}

func cloneScreen(screen [][]rune) [][]rune {
	out := make([][]rune, len(screen))
	for row := range screen {
		out[row] = append([]rune{}, screen[row]...)
	}
	return out
}

func resizeScreen(screen [][]rune, oldRows, oldCols, newRows, newCols int) [][]rune {
	resized := makeScreen(newRows, newCols)
	copyRows := min(min(oldRows, len(screen)), newRows)
	for row := 0; row < copyRows; row++ {
		copyCols := min(min(oldCols, len(screen[row])), newCols)
		for col := 0; col < copyCols; col++ {
			resized[row][col] = screen[row][col]
		}
	}
	return resized
}

func renderScreenLines(screen [][]rune) []string {
	lines := make([]string, len(screen))
	for index, row := range screen {
		lines[index] = string(trimRightSpaces(row))
	}
	return lines
}

func trimRightSpaces(row []rune) []rune {
	end := len(row)
	for end > 0 && row[end-1] == ' ' {
		end--
	}
	if end == 0 {
		return []rune{}
	}
	return row[:end]
}

func min(first, second int) int {
	if first < second {
		return first
	}
	return second
}

func max(first, second int) int {
	if first > second {
		return first
	}
	return second
}
