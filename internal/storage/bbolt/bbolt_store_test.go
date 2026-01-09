package bbolt

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/cloudprivacylabs/opencypher"
	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/testutil"
)

func TestRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	orig := testutil.SampleGraph()
	origNC := graph.NodeCount(orig)
	origEC := graph.EdgeCount(orig)

	if err := store.SaveGraph(orig); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}

	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	testutil.AssertNodeCount(t, loaded, origNC)
	testutil.AssertEdgeCount(t, loaded, origEC)

	ids := []string{
		"folder:src", "file:src/main.go", "file:src/utils.go",
		"func:main", "func:helper", "class:Service",
		"method:Service.Run", "community:0", "process:main-flow",
	}
	for _, id := range ids {
		testutil.AssertHasNode(t, loaded, id)
	}

	mainFile := graph.FindNodeByID(loaded, "file:src/main.go")
	if mainFile == nil {
		t.Fatal("main file node not found")
	}
	if lang := graph.GetStringProp(mainFile, graph.PropLanguage); lang != "go" {
		t.Errorf("expected language=go, got %q", lang)
	}

	helperFn := graph.FindNodeByID(loaded, "func:helper")
	if helperFn == nil {
		t.Fatal("helper function node not found")
	}
	if sl := graph.GetIntProp(helperFn, graph.PropStartLine); sl != 5 {
		t.Errorf("expected startLine=5, got %d", sl)
	}

	community := graph.FindNodeByID(loaded, "community:0")
	if community == nil {
		t.Fatal("community node not found")
	}
	if mod := graph.GetFloat64Prop(community, graph.PropModularity); mod != 0.45 {
		t.Errorf("expected modularity=0.45, got %f", mod)
	}

	// Edge types preserved: check that main calls helper.
	mainFn := graph.FindNodeByID(loaded, "func:main")
	if mainFn == nil {
		t.Fatal("main function node not found")
	}
	callEdges := graph.GetOutgoingEdges(mainFn, graph.RelCalls)
	if len(callEdges) == 0 {
		t.Error("expected at least one CALLS edge from main")
	}
}

func TestEmptyGraph(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	empty := lpg.NewGraph()
	if err := store.SaveGraph(empty); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}

	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	testutil.AssertNodeCount(t, loaded, 0)
	testutil.AssertEdgeCount(t, loaded, 0)
}

func TestOverwrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	graphA := testutil.SampleGraph()
	if err := store.SaveGraph(graphA); err != nil {
		t.Fatalf("SaveGraph A: %v", err)
	}

	graphB := lpg.NewGraph()
	graph.AddFileNode(graphB, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:b.go", Name: "b.go"},
		FilePath:      "b.go",
		Language:      "go",
	})
	if err := store.SaveGraph(graphB); err != nil {
		t.Fatalf("SaveGraph B: %v", err)
	}

	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	testutil.AssertNodeCount(t, loaded, 1)
	testutil.AssertEdgeCount(t, loaded, 0)
	testutil.AssertHasNode(t, loaded, "file:b.go")
}

func TestCloseAndReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	orig := testutil.SampleGraph()
	if err := store.SaveGraph(orig); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}
	store.Close()

	store2, err := New(dbPath)
	if err != nil {
		t.Fatalf("New (reopen): %v", err)
	}
	defer store2.Close()

	loaded, err := store2.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	testutil.AssertNodeCount(t, loaded, graph.NodeCount(orig))
	testutil.AssertEdgeCount(t, loaded, graph.EdgeCount(orig))
}

func TestLargeGraph(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	g := lpg.NewGraph()
	nodes := make([]*lpg.Node, 500)
	for i := range 500 {
		nodes[i] = graph.AddNode(g, graph.LabelFunction, map[string]any{
			graph.PropID:   fmt.Sprintf("func:%d", i),
			graph.PropName: fmt.Sprintf("func_%d", i),
		})
	}
	// Create ~1000 edges (each node connects to next two).
	for i := range 500 {
		graph.AddEdge(g, nodes[i], nodes[(i+1)%500], graph.RelCalls, nil)
		graph.AddEdge(g, nodes[i], nodes[(i+2)%500], graph.RelCalls, nil)
	}

	expNC := graph.NodeCount(g)
	expEC := graph.EdgeCount(g)

	if err := store.SaveGraph(g); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}

	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	testutil.AssertNodeCount(t, loaded, expNC)
	testutil.AssertEdgeCount(t, loaded, expEC)
}

func TestRoundTrip_CypherAfterLoad(t *testing.T) {
	// Verify round-tripped graph still works with Cypher.
	// LIMIT/SKIP broken in opencypher v1.0.0; tested without LIMIT here.
	dbPath := filepath.Join(t.TempDir(), "cypher.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	orig := testutil.SampleGraph()
	if err := store.SaveGraph(orig); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}

	loaded, err := store.LoadGraph()
	if err != nil {
		t.Fatalf("LoadGraph: %v", err)
	}

	testutil.AssertNodeCount(t, loaded, graph.NodeCount(orig))
	testutil.AssertEdgeCount(t, loaded, graph.EdgeCount(orig))

	ectx := opencypher.NewEvalContext(loaded)
	resVal, err := opencypher.ParseAndEvaluate("MATCH (n) RETURN n.name", ectx)
	if err != nil {
		t.Fatalf("Cypher on loaded graph: %v", err)
	}
	rs, ok := resVal.Get().(opencypher.ResultSet)
	if !ok {
		t.Fatalf("expected ResultSet, got %T", resVal.Get())
	}
	if len(rs.Rows) != graph.NodeCount(orig) {
		t.Errorf("expected %d rows, got %d", graph.NodeCount(orig), len(rs.Rows))
	}

	ectx2 := opencypher.NewEvalContext(loaded)
	resVal2, err := opencypher.ParseAndEvaluate("MATCH (n:Function) RETURN n.name", ectx2)
	if err != nil {
		t.Fatalf("Cypher label filter: %v", err)
	}
	rs2 := resVal2.Get().(opencypher.ResultSet)
	if len(rs2.Rows) != 2 {
		t.Errorf("expected 2 Function nodes, got %d", len(rs2.Rows))
	}

	ectx3 := opencypher.NewEvalContext(loaded)
	resVal3, err := opencypher.ParseAndEvaluate("MATCH (a)-[r]->(b) RETURN a.name, type(r), b.name", ectx3)
	if err != nil {
		t.Fatalf("Cypher relationship traversal: %v", err)
	}
	rs3 := resVal3.Get().(opencypher.ResultSet)
	t.Logf("Relationship traversal on loaded graph: %d rows", len(rs3.Rows))
	for i, row := range rs3.Rows {
		if i < 5 {
			for k, v := range row {
				t.Logf("  row[%d] %s = %v", i, k, v.Get())
			}
		}
	}
	if len(rs3.Rows) == 0 {
		t.Error("expected rows for relationship traversal on loaded graph")
	}
}
