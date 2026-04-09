package plugin

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

func TestLPGGraphBuilderAddNode(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})
	b.AddNode("Widget", "w:1", map[string]any{"name": "Sprocket"})

	if graph.NodeCount(g) != 1 {
		t.Fatalf("nodes = %d, want 1", graph.NodeCount(g))
	}
}

func TestLPGGraphBuilderAddEdge(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})
	b.AddNode("Owner", "o:1", nil)
	b.AddNode("Widget", "w:1", nil)
	b.AddEdge("o:1", "w:1", "OWNS", nil)

	if graph.EdgeCount(g) != 1 {
		t.Fatalf("edges = %d, want 1", graph.EdgeCount(g))
	}
}

func TestLPGGraphBuilderTransactionalCommit(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{Transactional: true})
	b.AddNode("Widget", "w:1", nil)
	b.AddNode("Owner", "o:1", nil)
	b.AddEdge("o:1", "w:1", "OWNS", nil)

	if graph.NodeCount(g) != 0 || graph.EdgeCount(g) != 0 {
		t.Fatal("expected no graph mutations before commit")
	}
	nodes, edges := b.Commit()
	if nodes != 2 || edges != 1 {
		t.Fatalf("commit = (%d, %d), want (2, 1)", nodes, edges)
	}
	if graph.NodeCount(g) != 2 || graph.EdgeCount(g) != 1 {
		t.Fatal("expected committed graph mutations")
	}
}

func TestLPGGraphBuilderTransactionalRollback(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{Transactional: true})
	b.AddNode("Widget", "w:1", nil)
	b.Rollback()

	if graph.NodeCount(g) != 0 {
		t.Fatal("expected rollback to discard staged nodes")
	}
}
