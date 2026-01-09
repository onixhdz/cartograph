//go:build !unix && !windows

package sysutil

import (
	"os"
	"syscall"
)

// DetachProcAttr returns a no-op SysProcAttr on unsupported platforms
// (e.g. js/wasm).
func DetachProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// SignalTerm is a no-op on unsupported platforms.
func SignalTerm(proc *os.Process) error {
	return proc.Kill()
}

// IsProcessRunning always returns false on unsupported platforms.
func IsProcessRunning(pid int) bool {
	return false
}
