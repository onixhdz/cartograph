package datasource

import (
	"fmt"
	"sync"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
)

// TestLPGGraphBuilder_ConcurrentAddNode tests that multiple goroutines can
// safely add nodes to the same builder without data loss or panics.
func TestLPGGraphBuilder_ConcurrentAddNode(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	const numWorkers = 10
	const nodesPerWorker = 100
	const totalNodes = numWorkers * nodesPerWorker

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			for i := range nodesPerWorker {
				id := fmt.Sprintf("node:%d:%d", workerID, i)
				b.AddNode("TestNode", id, map[string]any{
					"worker": workerID,
					"index":  i,
					"name":   fmt.Sprintf("node-%d-%d", workerID, i),
				})
			}
		}(w)
	}
	wg.Wait()

	if got := b.NodeCount(); got != totalNodes {
		t.Errorf("node count: got %d, want %d", got, totalNodes)
	}
}

// TestLPGGraphBuilder_ConcurrentAddEdge tests that multiple goroutines can
// safely add edges to the same builder. Nodes are pre-created, then edges
// are added concurrently.
func TestLPGGraphBuilder_ConcurrentAddEdge(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	const numNodes = 200
	// Create nodes first (sequential — not the point of this test).
	for i := range numNodes {
		b.AddNode("TestNode", fmt.Sprintf("node:%d", i), map[string]any{
			"name": fmt.Sprintf("node-%d", i),
		})
	}

	// Add edges concurrently: each worker connects a range of nodes.
	const numWorkers = 10
	edgesPerWorker := numNodes / numWorkers

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			start := workerID * edgesPerWorker
			for i := start; i < start+edgesPerWorker-1; i++ {
				fromID := fmt.Sprintf("node:%d", i)
				toID := fmt.Sprintf("node:%d", i+1)
				b.AddEdge(fromID, toID, "LINKS_TO", map[string]any{
					"worker": workerID,
				})
			}
		}(w)
	}
	wg.Wait()

	// Count edges in graph.
	edgeCount := 0
	for iter := g.GetEdges(); iter.Next(); {
		edgeCount++
	}

	// Each worker creates (edgesPerWorker - 1) edges.
	expectedEdges := numWorkers * (edgesPerWorker - 1)
	if edgeCount != expectedEdges {
		t.Errorf("edge count: got %d, want %d", edgeCount, expectedEdges)
	}
}

// TestLPGGraphBuilder_ConcurrentMixed tests concurrent node creation and
// edge creation happening simultaneously — the realistic scenario where
// notification handler goroutines emit both types concurrently.
func TestLPGGraphBuilder_ConcurrentMixed(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	const numPatterns = 500
	const numMitigations = 300

	// Phase 1: emit all nodes concurrently from 4 goroutines.
	var wg sync.WaitGroup
	wg.Add(4)

	// goroutine 1: patterns 0..249
	go func() {
		defer wg.Done()
		for i := range numPatterns / 2 {
			b.AddNode("CAPECPattern", fmt.Sprintf("capec:pattern:CAPEC-%d", i), map[string]any{
				"name": fmt.Sprintf("Pattern %d", i),
			})
		}
	}()

	// goroutine 2: patterns 250..499
	go func() {
		defer wg.Done()
		for i := numPatterns / 2; i < numPatterns; i++ {
			b.AddNode("CAPECPattern", fmt.Sprintf("capec:pattern:CAPEC-%d", i), map[string]any{
				"name": fmt.Sprintf("Pattern %d", i),
			})
		}
	}()

	// goroutine 3: mitigations 0..149
	go func() {
		defer wg.Done()
		for i := range numMitigations / 2 {
			b.AddNode("CAPECMitigation", fmt.Sprintf("capec:mitigation:COA-%d", i), map[string]any{
				"name": fmt.Sprintf("Mitigation %d", i),
			})
		}
	}()

	// goroutine 4: mitigations 150..299
	go func() {
		defer wg.Done()
		for i := numMitigations / 2; i < numMitigations; i++ {
			b.AddNode("CAPECMitigation", fmt.Sprintf("capec:mitigation:COA-%d", i), map[string]any{
				"name": fmt.Sprintf("Mitigation %d", i),
			})
		}
	}()

	wg.Wait()

	expectedNodes := numPatterns + numMitigations
	if got := b.NodeCount(); got != expectedNodes {
		t.Errorf("node count after phase 1: got %d, want %d", got, expectedNodes)
	}

	// Phase 2: emit edges concurrently.
	wg.Add(3)

	// CHILD_OF edges: pattern i → pattern i+1
	go func() {
		defer wg.Done()
		for i := range numPatterns - 1 {
			b.AddEdge(
				fmt.Sprintf("capec:pattern:CAPEC-%d", i),
				fmt.Sprintf("capec:pattern:CAPEC-%d", i+1),
				"CHILD_OF", nil,
			)
		}
	}()

	// MITIGATES edges: mitigation i → pattern i
	go func() {
		defer wg.Done()
		for i := range numMitigations {
			b.AddEdge(
				fmt.Sprintf("capec:mitigation:COA-%d", i),
				fmt.Sprintf("capec:pattern:CAPEC-%d", i),
				"MITIGATES", nil,
			)
		}
	}()

	// PEER_OF edges: pattern i → pattern (numPatterns-1-i)
	go func() {
		defer wg.Done()
		for i := range numPatterns / 2 {
			b.AddEdge(
				fmt.Sprintf("capec:pattern:CAPEC-%d", i),
				fmt.Sprintf("capec:pattern:CAPEC-%d", numPatterns-1-i),
				"PEER_OF", nil,
			)
		}
	}()

	wg.Wait()

	edgeCount := 0
	for iter := g.GetEdges(); iter.Next(); {
		edgeCount++
	}

	expectedEdges := (numPatterns - 1) + numMitigations + (numPatterns / 2)
	if edgeCount != expectedEdges {
		t.Errorf("edge count: got %d, want %d", edgeCount, expectedEdges)
	}
}

// TestLPGGraphBuilder_ConcurrentTransactional tests concurrent writes in
// transactional mode followed by a single Commit.
func TestLPGGraphBuilder_ConcurrentTransactional(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{Transactional: true})

	const numWorkers = 8
	const nodesPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			for i := range nodesPerWorker {
				b.AddNode("TestNode", fmt.Sprintf("node:%d:%d", workerID, i), map[string]any{
					"name": fmt.Sprintf("node-%d-%d", workerID, i),
				})
			}
		}(w)
	}
	wg.Wait()

	// Before commit: target graph should be empty.
	targetNodeCount := 0
	for iter := g.GetNodes(); iter.Next(); {
		targetNodeCount++
	}
	if targetNodeCount != 0 {
		t.Errorf("target nodes before commit: got %d, want 0", targetNodeCount)
	}

	// Commit.
	nodes, edges := b.Commit()
	expectedNodes := numWorkers * nodesPerWorker
	if nodes != expectedNodes {
		t.Errorf("committed nodes: got %d, want %d", nodes, expectedNodes)
	}
	if edges != 0 {
		t.Errorf("committed edges: got %d, want 0", edges)
	}
}

// BenchmarkLPGGraphBuilder_AddNode measures node emission throughput.
func BenchmarkLPGGraphBuilder_AddNode(b *testing.B) {
	g := lpg.NewGraph()
	builder := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})
	props := map[string]any{"name": "bench-node"}

	b.ResetTimer()
	for i := range b.N {
		builder.AddNode("BenchNode", fmt.Sprintf("bench:%d", i), props)
	}
}

// BenchmarkLPGGraphBuilder_AddNode_Concurrent measures concurrent node
// emission throughput from multiple goroutines.
func BenchmarkLPGGraphBuilder_AddNode_Concurrent(b *testing.B) {
	g := lpg.NewGraph()
	builder := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})
	props := map[string]any{"name": "bench-node"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			builder.AddNode("BenchNode", fmt.Sprintf("bench:%d:%d", i, b.N), props)
			i++
		}
	})
}

// BenchmarkLPGGraphBuilder_AddEdge_Concurrent measures concurrent edge
// emission throughput. Pre-creates nodes, then benchmarks edge creation.
func BenchmarkLPGGraphBuilder_AddEdge_Concurrent(b *testing.B) {
	g := lpg.NewGraph()
	builder := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	// Pre-create nodes.
	const numNodes = 10000
	for i := range numNodes {
		builder.AddNode("BenchNode", fmt.Sprintf("bench:%d", i), nil)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			from := fmt.Sprintf("bench:%d", i%numNodes)
			to := fmt.Sprintf("bench:%d", (i+1)%numNodes)
			builder.AddEdge(from, to, "LINKS_TO", nil)
			i++
		}
	})
}
