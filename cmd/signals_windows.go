//go:build windows

package cmd

import (
	"os"
	"syscall"
)

var nodeSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

func isStackDumpSignal(os.Signal) bool { return false }
func isRestartSignal(os.Signal) bool   { return false }
