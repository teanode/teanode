//go:build darwin

package terminals

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TermState holds the original terminal settings for later restore.
type TermState struct {
	termios *unix.Termios
}

// OpenPTY opens a new PTY master/slave pair.
func OpenPTY() (master, slave *os.File, err error) {
	masterFile, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	defer func() {
		if err != nil {
			masterFile.Close()
		}
	}()

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, masterFile.Fd(), uintptr(unix.TIOCPTYGRANT), 0); errno != 0 {
		return nil, nil, fmt.Errorf("TIOCPTYGRANT: %w", errno)
	}
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, masterFile.Fd(), uintptr(unix.TIOCPTYUNLK), 0); errno != 0 {
		return nil, nil, fmt.Errorf("TIOCPTYUNLK: %w", errno)
	}

	// The argument size is encoded in the ioctl request.
	const iocparmMask = 0x1fff
	nameLen := (unix.TIOCPTYGNAME >> 16) & iocparmMask
	nameBuffer := make([]byte, nameLen)
	if _, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		masterFile.Fd(),
		uintptr(unix.TIOCPTYGNAME),
		uintptr(unsafe.Pointer(&nameBuffer[0])),
	); errno != 0 {
		return nil, nil, fmt.Errorf("TIOCPTYGNAME: %w", errno)
	}

	nameEnd := bytes.IndexByte(nameBuffer, 0)
	if nameEnd < 0 {
		nameEnd = len(nameBuffer)
	}
	slavePath := string(nameBuffer[:nameEnd])
	if slavePath == "" {
		return nil, nil, fmt.Errorf("TIOCPTYGNAME: empty slave path")
	}

	slaveFile, err := os.OpenFile(slavePath, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open slave %s: %w", slavePath, err)
	}

	return masterFile, slaveFile, nil
}

// SetWinSize sets the terminal window size on a file descriptor.
func SetWinSize(fd int, rows, cols uint16) error {
	windowSize := unix.Winsize{Row: rows, Col: cols}
	return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &windowSize)
}

// GetWinSize returns the terminal window size for a file descriptor.
func GetWinSize(fd int) (rows, cols uint16, err error) {
	windowSize, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return windowSize.Row, windowSize.Col, nil
}

// MakeRaw puts the terminal into raw mode and returns the original state for later restore.
func MakeRaw(fd int) (*TermState, error) {
	orig, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	raw := *orig
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return nil, err
	}
	return &TermState{termios: orig}, nil
}

// RestoreTermios restores the terminal to its original mode.
func RestoreTermios(fd int, state *TermState) {
	if state != nil && state.termios != nil {
		_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, state.termios)
	}
}
