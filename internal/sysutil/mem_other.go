//go:build !linux && !darwin && !windows

package sysutil

// AvailableMemory returns 0 on unsupported platforms, causing callers
// to fall back to CPU-only heuristics.
func AvailableMemory() uint64 { return 0 }
