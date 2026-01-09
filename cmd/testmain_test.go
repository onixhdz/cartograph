package cmd

import (
	"os"
	"runtime/debug"
	"testing"
)

// TestMain sets up resource guardrails (GOMEMLIMIT=1GiB, GOGC=50,
// single embedding worker) to prevent OOM in test environments.
func TestMain(m *testing.M) {
	// Cap Go heap to 1 GiB. This triggers GC more aggressively when
	// tests approach the limit rather than growing unbounded until the
	// kernel OOM-killer nukes the container.
	const memLimit = 1 << 30 // 1 GiB
	debug.SetMemoryLimit(memLimit)

	// Force conservative GC — the default 100% means the heap can
	// double before GC fires, which is dangerous with a 100+ MB binary.
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}

	// Ensure embedding never spins up more than one WASM worker if a
	// test accidentally triggers it.
	if os.Getenv("CARTOGRAPH_EMBEDDING_WORKERS") == "" {
		os.Setenv("CARTOGRAPH_EMBEDDING_WORKERS", "1")
	}

	os.Exit(m.Run())
}
