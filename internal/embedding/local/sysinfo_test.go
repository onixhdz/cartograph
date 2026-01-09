package local

import (
	"runtime"
	"testing"

	"github.com/realxen/cartograph/internal/sysutil"
)

func TestDefaultWorkerCount(t *testing.T) {
	// Without env override, should return ≥ 1 and ≤ min(numCPU, 8).
	n := defaultWorkerCount()
	cpus := runtime.NumCPU()

	if n < 1 {
		t.Errorf("defaultWorkerCount() = %d, want >= 1", n)
	}
	maxExpected := min(cpus, 8)
	if n > maxExpected {
		t.Errorf("defaultWorkerCount() = %d, want <= %d (numCPU=%d)", n, maxExpected, cpus)
	}
	t.Logf("numCPU=%d, availMem=%d MB, workers=%d", cpus, sysutil.AvailableMemory()/(1<<20), n)
}

func TestDefaultWorkerCount_EnvOverride(t *testing.T) {
	// CARTOGRAPH_EMBEDDING_WORKERS should override the heuristic.
	t.Setenv("CARTOGRAPH_EMBEDDING_WORKERS", "3")
	if n := defaultWorkerCount(); n != 3 {
		t.Errorf("with CARTOGRAPH_EMBEDDING_WORKERS=3, got %d", n)
	}

	// Invalid values should fall back to heuristic.
	t.Setenv("CARTOGRAPH_EMBEDDING_WORKERS", "0")
	if n := defaultWorkerCount(); n < 1 {
		t.Errorf("with CARTOGRAPH_EMBEDDING_WORKERS=0 (invalid), got %d, want >= 1", n)
	}

	t.Setenv("CARTOGRAPH_EMBEDDING_WORKERS", "notanumber")
	if n := defaultWorkerCount(); n < 1 {
		t.Errorf("with CARTOGRAPH_EMBEDDING_WORKERS=notanumber, got %d, want >= 1", n)
	}
}
