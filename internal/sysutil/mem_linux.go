//go:build linux

package sysutil

import "golang.org/x/sys/unix"

// AvailableMemory returns the system's free memory in bytes via the
// sysinfo(2) syscall. Freeram * Unit gives usable bytes. Returns 0 on
// any error.
func AvailableMemory() uint64 {
	var si unix.Sysinfo_t
	if err := unix.Sysinfo(&si); err != nil {
		return 0
	}
	return si.Freeram * uint64(si.Unit)
}
