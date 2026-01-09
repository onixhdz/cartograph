package extractors

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// TestParseTimings is a manual benchmark for serial vs parallel parse overhead.
// Run explicitly with: go test -run TestParseTimings -v
func TestParseTimings(t *testing.T) {
	t.Skip("manual benchmark: run explicitly with -run TestParseTimings when needed")
	root := "/tmp/temporal-bench"
	if _, err := os.Stat(root); err != nil {
		t.Skip("temporal-bench not cloned at /tmp/temporal-bench")
	}

	// Collect Go files (non-test, cap at 500 for speed).
	type srcFile struct {
		path string
		data []byte
	}
	var files []srcFile
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			n := info.Name()
			if n == "vendor" || n == ".git" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= 500 {
			return filepath.SkipAll
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			data, err := os.ReadFile(path)
			if err == nil && len(data) > 0 {
				rel, _ := filepath.Rel(root, path)
				files = append(files, srcFile{rel, data})
			}
		}
		return nil
	})
	t.Logf("loaded %d files", len(files))

	lang := grammars.DetectLanguageByName("go").Language()
	pool := ts.NewParserPool(lang)

	// --- Measure Parse-only time (serial) ---
	start := time.Now()
	trees := make([]*ts.Tree, len(files))
	for i, f := range files {
		trees[i], _ = pool.Parse(f.data)
	}
	parseSerial := time.Since(start)
	t.Logf("SERIAL Parse-only  (%d files): %v  (%.1f ms/file)", len(files), parseSerial, float64(parseSerial.Milliseconds())/float64(len(files)))

	// --- Measure Execute-only time (serial) ---
	q, _ := ts.NewQuery(goQueries, lang)
	start = time.Now()
	totalMatches := 0
	for _, tr := range trees {
		if tr != nil {
			totalMatches += len(q.Execute(tr))
		}
	}
	execSerial := time.Since(start)
	t.Logf("SERIAL Execute-only (%d files): %v  (%.1f ms/file, %d matches)", len(files), execSerial, float64(execSerial.Milliseconds())/float64(len(files)), totalMatches)

	// --- Measure full serial (parse+execute, one at a time) ---
	start = time.Now()
	for _, f := range files {
		tr, err := pool.Parse(f.data)
		if err != nil {
			continue
		}
		q2, _ := ts.NewQuery(goQueries, lang)
		q2.Execute(tr)
	}
	fullSerial := time.Since(start)
	t.Logf("SERIAL Parse+Execute (%d files): %v", len(files), fullSerial)

	// --- Measure parallel with mutex on parse (our current approach) ---
	var mu sync.Mutex
	start = time.Now()
	{
		var wg sync.WaitGroup
		work := make(chan int, len(files))
		for j := range files {
			work <- j
		}
		close(work)
		for w := 0; w < runtime.NumCPU(); w++ {
			wg.Go(func() {
				for j := range work {
					mu.Lock()
					tr, err := pool.Parse(files[j].data)
					mu.Unlock()
					if err != nil {
						continue
					}
					q2, _ := ts.NewQuery(goQueries, lang)
					q2.Execute(tr)
				}
			})
		}
		wg.Wait()
	}
	parallelMutex := time.Since(start)
	t.Logf("PARALLEL Parse(mutex)+Execute (%d files, %d workers): %v", len(files), runtime.NumCPU(), parallelMutex)

	// --- Measure parallel no mutex (fast but lossy) ---
	start = time.Now()
	{
		var wg sync.WaitGroup
		work := make(chan int, len(files))
		for j := range files {
			work <- j
		}
		close(work)
		for w := 0; w < runtime.NumCPU(); w++ {
			wg.Go(func() {
				for j := range work {
					tr, err := pool.Parse(files[j].data)
					if err != nil {
						continue
					}
					q2, _ := ts.NewQuery(goQueries, lang)
					q2.Execute(tr)
				}
			})
		}
		wg.Wait()
	}
	parallelNoMutex := time.Since(start)
	t.Logf("PARALLEL Parse+Execute no-mutex (%d files, %d workers): %v", len(files), runtime.NumCPU(), parallelNoMutex)

	// --- Summary ---
	t.Logf("")
	t.Logf("=== SUMMARY (%d files, %d CPUs) ===", len(files), runtime.NumCPU())
	t.Logf("  Parse-only serial:        %v", parseSerial)
	t.Logf("  Execute-only serial:      %v", execSerial)
	t.Logf("  Full serial:              %v", fullSerial)
	t.Logf("  Parallel mutex(parse):    %v  (%.1fx vs no-mutex)", parallelMutex, float64(parallelMutex)/float64(parallelNoMutex))
	t.Logf("  Parallel no-mutex:        %v  (baseline)", parallelNoMutex)
	t.Logf("  Mutex overhead:           %v", parallelMutex-parallelNoMutex)

	speedup := float64(fullSerial) / float64(parallelMutex)
	t.Logf("  Speedup vs full serial:   %.1fx", speedup)

	fmt.Println() // force flush
}
