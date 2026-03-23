//go:build !windows

package cmd

import "golang.org/x/sys/unix"

// dup2 wraps unix.Dup2 which handles platforms where the dup2 syscall is
// absent (e.g. Linux arm64, which only has dup3).
func dup2(oldfd, newfd int) error {
	return unix.Dup2(oldfd, newfd)
}
