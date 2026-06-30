package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/onixhdz/cartograph/plugin"
	"github.com/onixhdz/cartograph/plugin/plugintest"
)

// TestConcurrent_MultiplePluginInstances launches N CAPEC plugin instances
// concurrently and verifies all produce correct results independently.
func TestConcurrent_MultiplePluginInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent plugin test in short mode")
	}

	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	const numPlugins = 5
	hosts := make([]*plugintest.Host, numPlugins)
	errs := make([]error, numPlugins)

	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(numPlugins)
	for i := range numPlugins {
		go func(idx int) {
			defer wg.Done()
			host := plugintest.NewHost(plugintest.Config{"stix_url": srv.URL + "/stix-capec.json"})
			hosts[idx] = host
			_, errs[idx] = (&capecPlugin{}).Ingest(context.Background(), host, plugin.IngestOptions{})
		}(i)
	}
	wg.Wait()

	elapsed := time.Since(start)
	t.Logf("%d concurrent plugin instances completed in %v (avg %v/plugin)", numPlugins, elapsed, elapsed/time.Duration(numPlugins))

	for i := range numPlugins {
		if errs[i] != nil {
			t.Errorf("plugin %d error: %v", i, errs[i])
			continue
		}
		h := hosts[i]
		h.AssertNodeCount(t, 6)
		h.AssertEdgeCount(t, 7)
		h.AssertNodeExists(t, patternNodeID(testCapecSQLInjection), labelPattern)
		h.AssertEdgeExists(t, patternNodeID(testCapecSQLInjection), patternNodeID(testCapecCategoryMeta), edgeChildOf)
	}
}

// TestConcurrent_SharedHostEmission tests multiple goroutines emitting
// nodes and edges through a single plugintest.Host. This simulates the
// JSON-RPC notification handler goroutines within a single plugin process.
func TestConcurrent_SharedHostEmission(t *testing.T) {
	host := plugintest.NewHost(nil)
	ctx := context.Background()

	const numWorkers = 8
	const nodesPerWorker = 100
	const edgesPerWorker = 50

	var wg sync.WaitGroup
	wg.Add(numWorkers * 2) // node workers + edge workers

	// Node workers.
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			for i := range nodesPerWorker {
				id := fmt.Sprintf("node:%d:%d", workerID, i)
				err := host.EmitNode(ctx, plugin.Node{
					ID:    id,
					Label: "TestNode",
					Properties: map[string]any{
						"name": fmt.Sprintf("node-%d-%d", workerID, i),
					},
				})
				if err != nil {
					t.Errorf("worker %d: EmitNode: %v", workerID, err)
					return
				}
			}
		}(w)
	}

	// Edge workers (use sequential IDs that overlap between workers to
	// exercise concurrent map access in the host).
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			for i := range edgesPerWorker {
				fromID := fmt.Sprintf("node:%d:%d", workerID, i)
				toID := fmt.Sprintf("node:%d:%d", workerID, i+1)
				err := host.EmitEdge(ctx, plugin.Edge{From: fromID, To: toID, Type: "LINKS_TO"})
				if err != nil {
					t.Errorf("worker %d: EmitEdge: %v", workerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()

	expectedNodes := numWorkers * nodesPerWorker
	expectedEdges := numWorkers * edgesPerWorker

	host.AssertNodeCount(t, expectedNodes)
	host.AssertEdgeCount(t, expectedEdges)

	t.Logf("emitted %d nodes, %d edges from %d concurrent workers", expectedNodes, expectedEdges, numWorkers)
}

// TestConcurrent_HighVolumeInProcess tests a single CAPEC plugin instance
// processing a large fixture through plugintest.Host, verifying correctness
// and measuring throughput for in-process mode.
func TestConcurrent_HighVolumeInProcess(t *testing.T) {
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	// Run the plugin many times in parallel against separate hosts.
	const numRuns = 20
	results := make([]plugin.IngestResult, numRuns)
	hosts := make([]*plugintest.Host, numRuns)

	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(numRuns)
	for i := range numRuns {
		go func(idx int) {
			defer wg.Done()
			h := plugintest.NewHost(plugintest.Config{
				"stix_url": srv.URL + "/capec.json",
			})
			hosts[idx] = h

			p := &capecPlugin{}
			ctx := context.Background()
			r, err := p.Ingest(ctx, h, plugin.IngestOptions{})
			if err != nil {
				t.Errorf("run %d: Ingest: %v", idx, err)
				return
			}
			results[idx] = r
		}(i)
	}
	wg.Wait()

	elapsed := time.Since(start)
	t.Logf("%d concurrent in-process runs completed in %v (avg %v/run)", numRuns, elapsed, elapsed/time.Duration(numRuns))

	// Verify all results.
	for i := range numRuns {
		if results[i].Nodes != 6 {
			t.Errorf("run %d: nodes = %d, want 6", i, results[i].Nodes)
		}
		if results[i].Edges != 7 {
			t.Errorf("run %d: edges = %d, want 7", i, results[i].Edges)
		}
		hosts[i].AssertNodeCount(t, 6)
		hosts[i].AssertEdgeCount(t, 7)
	}
}

// BenchmarkPluginIngest_InProcess benchmarks a single CAPEC ingest cycle
// using plugintest.Host (no subprocess, no JSON-RPC overhead).
func BenchmarkPluginIngest_InProcess(b *testing.B) {
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	b.ResetTimer()
	for range b.N {
		h := plugintest.NewHost(plugintest.Config{
			"stix_url": srv.URL + "/capec.json",
		})

		p := &capecPlugin{}
		ctx := context.Background()
		_, err := p.Ingest(ctx, h, plugin.IngestOptions{})
		if err != nil {
			b.Fatalf("Ingest: %v", err)
		}
	}
}

// BenchmarkPluginIngest_InProcess_Parallel benchmarks concurrent CAPEC
// ingest cycles.
func BenchmarkPluginIngest_InProcess_Parallel(b *testing.B) {
	fixtureData, err := os.ReadFile("testdata/mini-capec.json")
	if err != nil {
		b.Fatalf("read fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureData)
	}))
	defer srv.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			h := plugintest.NewHost(plugintest.Config{
				"stix_url": srv.URL + "/capec.json",
			})

			p := &capecPlugin{}
			ctx := context.Background()
			_, err := p.Ingest(ctx, h, plugin.IngestOptions{})
			if err != nil {
				b.Fatalf("Ingest: %v", err)
			}
		}
	})
}
