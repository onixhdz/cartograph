package bbolt

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/realxen/cartograph/internal/graph"
)

func buildBenchGraph(nodeCount, edgesPerNode int) *lpg.Graph {
	g := lpg.NewGraph()
	nodes := make([]*lpg.Node, nodeCount)
	for i := range nodeCount {
		nodes[i] = graph.AddNode(g, graph.LabelFunction, map[string]any{
			graph.PropID:        fmt.Sprintf("func:%d", i),
			graph.PropName:      fmt.Sprintf("functionName_%d", i),
			graph.PropFilePath:  fmt.Sprintf("src/pkg%d/file%d.go", i/50, i),
			graph.PropStartLine: i * 10,
			graph.PropEndLine:   i*10 + 20,
			graph.PropContent:   fmt.Sprintf("func f%d() { return %d }", i, i),
		})
	}
	for i := range nodeCount {
		for e := 1; e <= edgesPerNode; e++ {
			graph.AddEdge(g, nodes[i], nodes[(i+e)%nodeCount], graph.RelCalls, nil)
		}
	}
	return g
}

func BenchmarkSaveGraph(b *testing.B) {
	for _, size := range []int{500, 2000, 5000} {
		g := buildBenchGraph(size, 2)
		b.Run(fmt.Sprintf("nodes=%d", size), func(b *testing.B) {
			for b.Loop() {
				dir := b.TempDir()
				store, err := New(filepath.Join(dir, "bench.db"))
				if err != nil {
					b.Fatal(err)
				}
				if err := store.SaveGraph(g); err != nil {
					b.Fatal(err)
				}
				_ = store.Close()
			}
		})
	}
}

// TestSaveGraphProfile breaks down where time goes in SaveGraph.
func TestSaveGraphProfile(t *testing.T) {
	sizes := []int{500, 2000, 5000}
	for _, size := range sizes {
		g := buildBenchGraph(size, 2)
		nc := graph.NodeCount(g)
		ec := graph.EdgeCount(g)

		dir := t.TempDir()
		dbPath := filepath.Join(dir, "profile.db")

		// Phase 1: measure serialization only (no bbolt)
		start := time.Now()
		props := make(map[string]any, 16)
		graph.ForEachNode(g, func(node *lpg.Node) bool {
			clear(props)
			node.ForEachProperty(func(key string, value any) bool {
				props[key] = value
				return true
			})
			props["_labels"] = node.GetLabels().Slice()
			props["_id"] = props[graph.PropID]
			_, _ = msgpack.Marshal(props)
			return true
		})
		graph.ForEachEdge(g, func(edge *lpg.Edge) bool {
			clear(props)
			edge.ForEachProperty(func(key string, value any) bool {
				props[key] = value
				return true
			})
			props["_from"] = graph.GetStringProp(edge.GetFrom(), graph.PropID)
			props["_to"] = graph.GetStringProp(edge.GetTo(), graph.PropID)
			props["_label"] = edge.GetLabel()
			_, _ = msgpack.Marshal(props)
			return true
		})
		serializeTime := time.Since(start)

		// Phase 2: measure full SaveGraph (serialize + bbolt write + fsync)
		store, err := New(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		start = time.Now()
		if err := store.SaveGraph(g); err != nil {
			t.Fatal(err)
		}
		saveTime := time.Since(start)
		fi, _ := os.Stat(dbPath)
		dbSize := fi.Size()
		_ = store.Close()

		bboltTime := saveTime - serializeTime
		t.Logf("size=%d  nodes=%d edges=%d  db=%.1fKB", size, nc, ec, float64(dbSize)/1024)
		t.Logf("  serialize: %v  (%.0f%%)", serializeTime, 100*float64(serializeTime)/float64(saveTime))
		t.Logf("  bbolt I/O: %v  (%.0f%%)", bboltTime, 100*float64(bboltTime)/float64(saveTime))
		t.Logf("  total:     %v", saveTime)
	}
}
