package sysutil

import (
	"os"
	"testing"
)

func TestAvailableMemory(t *testing.T) {
	mem := AvailableMemory()
	if mem == 0 {
		if _, err := os.Stat("/proc/meminfo"); err == nil {
			t.Error("on Linux with /proc/meminfo, expected non-zero available memory")
		} else {
			t.Log("AvailableMemory() = 0 (expected on non-Linux or constrained env)")
		}
		return
	}
	// Sanity: should be at least 100 MB and less than 1 TB.
	if mem < 100<<20 {
		t.Errorf("available memory = %d bytes, suspiciously low", mem)
	}
	if mem > 1<<40 {
		t.Errorf("available memory = %d bytes, suspiciously high", mem)
	}
	t.Logf("available memory: %d MB", mem/(1<<20))
}
