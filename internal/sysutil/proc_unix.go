//go:build unix

package sysutil

import (
	"os"
	"syscall"
)

// DetachProcAttr returns SysProcAttr that detaches the child process
// from the parent's process group so it survives the parent exiting.
// On Unix, this creates a new session via Setsid.
func DetachProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setsid: true, // Create a new session (detach from terminal).
	}
}

// SignalTerm sends SIGTERM to the process for a graceful shutdown.
func SignalTerm(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

// IsProcessRunning checks if a process with the given PID exists by
// sending signal 0 (no actual signal is delivered).
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
