//go:build linux

package terminals

import (
	"fmt"
	"os"
	"strconv"
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

	// Unlock the slave.
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, masterFile.Fd(), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(new(int32)))); errno != 0 {
		masterFile.Close()
		return nil, nil, fmt.Errorf("TIOCSPTLCK: %w", errno)
	}

	// Get slave number.
	var slaveNumber uint32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, masterFile.Fd(), unix.TIOCGPTN, uintptr(unsafe.Pointer(&slaveNumber))); errno != 0 {
		masterFile.Close()
		return nil, nil, fmt.Errorf("TIOCGPTN: %w", errno)
	}

	slavePath := "/dev/pts/" + strconv.Itoa(int(slaveNumber))
	slaveFile, err := os.OpenFile(slavePath, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		masterFile.Close()
		return nil, nil, fmt.Errorf("open slave %s: %w", slavePath, err)
	}

	return masterFile, slaveFile, nil
}

// SetWinSize sets the terminal window size on a file descriptor.
func SetWinSize(fileDescriptor int, rows, cols uint16) error {
	windowSize := unix.Winsize{Row: rows, Col: cols}
	return unix.IoctlSetWinsize(fileDescriptor, unix.TIOCSWINSZ, &windowSize)
}

// GetWinSize returns the terminal window size for a file descriptor.
func GetWinSize(fileDescriptor int) (rows, cols uint16, err error) {
	windowSize, err := unix.IoctlGetWinsize(fileDescriptor, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, err
	}
	return windowSize.Row, windowSize.Col, nil
}

// MakeRaw puts the terminal into raw mode and returns the original state for later restore.
func MakeRaw(fileDescriptor int) (*TermState, error) {
	original, err := unix.IoctlGetTermios(fileDescriptor, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	raw := *original
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, &raw); err != nil {
		return nil, err
	}
	return &TermState{termios: original}, nil
}

// RestoreTermios restores the terminal to its original mode.
func RestoreTermios(fileDescriptor int, state *TermState) {
	if state != nil && state.termios != nil {
		_ = unix.IoctlSetTermios(fileDescriptor, unix.TCSETS, state.termios)
	}
}
