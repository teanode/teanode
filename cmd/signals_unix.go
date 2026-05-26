//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

var nodeSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP}

func isStackDumpSignal(signal os.Signal) bool { return signal == syscall.SIGQUIT }
func isRestartSignal(signal os.Signal) bool   { return signal == syscall.SIGHUP }
