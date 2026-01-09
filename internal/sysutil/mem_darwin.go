//go:build darwin

package sysutil

import "golang.org/x/sys/unix"

// AvailableMemory returns total physical memory in bytes via sysctl(hw.memsize).
// macOS lacks a direct "available" metric, so this is the upper bound.
func AvailableMemory() uint64 {
	val, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return val
}
