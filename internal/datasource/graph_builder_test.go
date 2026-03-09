package datasource

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

func TestLPGGraphBuilderAddNode(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	b.AddNode("AwsEc2Instance", "i-123", map[string]any{
		"name":   "web-1",
		"region": "us-east-1",
	})

	if b.NodeCount() != 1 {
		t.Fatalf("NodeCount = %d, want 1", b.NodeCount())
	}

	node := graph.FindNodeByID(g, "i-123")
	if node == nil {
		t.Fatal("node not found by ID")
	}
	if !node.HasLabel("AwsEc2Instance") {
		t.Error("node should have vendor label")
	}
	if v, _ := node.GetProperty("name"); v != "web-1" {
		t.Errorf("name = %v, want web-1", v)
	}
}

func TestLPGGraphBuilderDualLabels(t *testing.T) {
	g := lpg.NewGraph()
	resolver := func(vendorLabel string) ResourceKind {
		if vendorLabel == "AwsEc2Instance" {
			return KindVirtualMachine
		}
		return ""
	}

	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{
		KindResolver: resolver,
	})

	b.AddNode("AwsEc2Instance", "i-456", map[string]any{"name": "db-1"})

	node := graph.FindNodeByID(g, "i-456")
	if node == nil {
		t.Fatal("node not found")
	}
	if !node.HasLabel("AwsEc2Instance") {
		t.Error("missing vendor label")
	}
	if !node.HasLabel("VirtualMachine") {
		t.Error("missing normalized kind label")
	}
}

func TestLPGGraphBuilderDedup(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	b.AddNode("AwsEc2Instance", "i-dup", map[string]any{
		"name":   "web-1",
		"region": "us-east-1",
	})
	b.AddNode("AwsEc2Instance", "i-dup", map[string]any{
		"name":   "web-1-updated",
		"status": "running",
	})

	if b.NodeCount() != 1 {
		t.Fatalf("NodeCount = %d, want 1 (dedup)", b.NodeCount())
	}

	node := graph.FindNodeByID(g, "i-dup")
	if node == nil {
		t.Fatal("node not found")
	}

	// Last write wins for properties.
	if v, _ := node.GetProperty("name"); v != "web-1-updated" {
		t.Errorf("name = %v, want web-1-updated", v)
	}
	// Original properties retained.
	if v, _ := node.GetProperty("region"); v != "us-east-1" {
		t.Errorf("region = %v, want us-east-1", v)
	}
	// New properties added.
	if v, _ := node.GetProperty("status"); v != "running" {
		t.Errorf("status = %v, want running", v)
	}
}

func TestLPGGraphBuilderAddEdge(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	b.AddNode("AwsVpc", "vpc-1", map[string]any{"name": "main"})
	b.AddNode("AwsSubnet", "subnet-1", map[string]any{"name": "public"})
	b.AddEdge("vpc-1", "subnet-1", "CONTAINS", map[string]any{"az": "us-east-1a"})

	// Verify the edge exists.
	from := graph.FindNodeByID(g, "vpc-1")
	to := graph.FindNodeByID(g, "subnet-1")
	if from == nil || to == nil {
		t.Fatal("nodes not found")
	}

	edges := graph.GetOutgoingEdges(from, graph.RelType("CONTAINS"))
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	if edges[0].GetTo() != to {
		t.Error("edge target mismatch")
	}
}

func TestLPGGraphBuilderEdgeMissingNode(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	b.AddNode("AwsVpc", "vpc-1", nil)
	// Edge to nonexistent node should be silently dropped.
	b.AddEdge("vpc-1", "nonexistent", "CONTAINS", nil)

	from := graph.FindNodeByID(g, "vpc-1")
	if from == nil {
		t.Fatal("node not found")
	}
	edges := graph.GetOutgoingEdges(from, graph.RelType("CONTAINS"))
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

func TestLPGGraphBuilderTransactionalCommit(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{Transactional: true})

	b.AddNode("AwsEc2Instance", "i-tx1", map[string]any{"name": "tx-test"})
	b.AddNode("AwsEc2Instance", "i-tx2", map[string]any{"name": "tx-test-2"})
	b.AddEdge("i-tx1", "i-tx2", "DEPENDS_ON", nil)

	// Before commit, target graph should be empty.
	if graph.NodeCount(g) != 0 {
		t.Errorf("target should be empty before commit, got %d nodes", graph.NodeCount(g))
	}

	nodes, edges := b.Commit()
	if nodes != 2 {
		t.Errorf("committed %d nodes, want 2", nodes)
	}
	if edges != 1 {
		t.Errorf("committed %d edges, want 1", edges)
	}

	// After commit, target graph should have the nodes.
	if graph.NodeCount(g) != 2 {
		t.Errorf("target has %d nodes after commit, want 2", graph.NodeCount(g))
	}
	if graph.EdgeCount(g) != 1 {
		t.Errorf("target has %d edges after commit, want 1", graph.EdgeCount(g))
	}
}

func TestLPGGraphBuilderTransactionalRollback(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{Transactional: true})

	b.AddNode("AwsEc2Instance", "i-rb1", map[string]any{"name": "rb-test"})
	b.Rollback()

	// After rollback, both target and builder should be clean.
	if graph.NodeCount(g) != 0 {
		t.Errorf("target has %d nodes after rollback, want 0", graph.NodeCount(g))
	}
	if b.NodeCount() != 0 {
		t.Errorf("builder has %d nodes after rollback, want 0", b.NodeCount())
	}
}

func TestLPGGraphBuilderNonTxCommit(t *testing.T) {
	g := lpg.NewGraph()
	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{})

	b.AddNode("X", "x-1", nil)

	// Commit on non-transactional builder should be a no-op.
	nodes, edges := b.Commit()
	if nodes != 0 || edges != 0 {
		t.Errorf("non-tx Commit returned nodes=%d edges=%d, want 0,0", nodes, edges)
	}

	// Node should already be in the target.
	if graph.NodeCount(g) != 1 {
		t.Errorf("target has %d nodes, want 1", graph.NodeCount(g))
	}
}

func TestLPGGraphBuilderDedupWithDualLabels(t *testing.T) {
	g := lpg.NewGraph()
	resolver := func(vendorLabel string) ResourceKind {
		switch vendorLabel {
		case "AwsEc2Instance":
			return KindVirtualMachine
		case "GcpComputeInstance":
			return KindVirtualMachine
		}
		return ""
	}

	b := NewLPGGraphBuilder(g, LPGGraphBuilderOptions{KindResolver: resolver})

	// First emit with AWS label.
	b.AddNode("AwsEc2Instance", "vm-shared", map[string]any{"name": "shared"})
	// Second emit with additional label (dedup + label union).
	b.AddNode("GcpComputeInstance", "vm-shared", map[string]any{"cloud": "gcp"})

	if b.NodeCount() != 1 {
		t.Fatalf("NodeCount = %d, want 1", b.NodeCount())
	}

	node := graph.FindNodeByID(g, "vm-shared")
	if node == nil {
		t.Fatal("node not found")
	}
	if !node.HasLabel("AwsEc2Instance") {
		t.Error("missing AwsEc2Instance label")
	}
	if !node.HasLabel("GcpComputeInstance") {
		t.Error("missing GcpComputeInstance label")
	}
	if !node.HasLabel("VirtualMachine") {
		t.Error("missing VirtualMachine label")
	}
}
