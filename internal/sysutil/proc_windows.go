//go:build windows

package sysutil

import (
	"os"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// DetachProcAttr returns SysProcAttr that detaches the child process on
// Windows using CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS.
func DetachProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess,
	}
}

// SignalTerm sends os.Interrupt on Windows (no SIGTERM support).
// If it fails (common for detached processes), the caller should use Kill().
func SignalTerm(proc *os.Process) error {
	return proc.Signal(os.Interrupt)
}

// IsProcessRunning checks if a process with the given PID exists.
// On Windows (Go 1.22+), Signal(0) opens the process handle and
// returns nil if the process is alive.
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
