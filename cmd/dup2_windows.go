//go:build windows

package cmd

import "fmt"

func dup2(oldfd, newfd int) error {
	return fmt.Errorf("dup2 is not supported on Windows")
}
