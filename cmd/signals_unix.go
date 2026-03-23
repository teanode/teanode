//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

var nodeSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP}

func isStackDumpSignal(sig os.Signal) bool { return sig == syscall.SIGQUIT }
func isRestartSignal(sig os.Signal) bool   { return sig == syscall.SIGHUP }
