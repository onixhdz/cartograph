// Package testutil provides shared test fixtures and helpers for
// Cartograph unit tests.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// SampleGraph builds a small representative knowledge graph for testing.
// Contains Folder, File, Function, Class, Method, Community, and Process nodes
// with CONTAINS, CALLS, HAS_METHOD, MEMBER_OF, and STEP_IN_PROCESS edges.
func SampleGraph() *lpg.Graph {
	g := lpg.NewGraph()

	folder := graph.AddFolderNode(g, graph.FolderProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "folder:src", Name: "src"},
		FilePath:      "src/",
	})

	mainFile := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main.go", Name: "main.go"},
		FilePath:      "src/main.go",
		Language:      "go",
		Size:          1024,
	})
	utilsFile := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils.go", Name: "utils.go"},
		FilePath:      "src/utils.go",
		Language:      "go",
		Size:          512,
	})

	graph.AddEdge(g, folder, mainFile, graph.RelContains, nil)
	graph.AddEdge(g, folder, utilsFile, graph.RelContains, nil)

	mainFn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "src/main.go",
		StartLine:     10,
		EndLine:       25,
		IsExported:    false,
		Content:       "func main() { ... }",
		Signature:     "func main()",
	})
	helperFn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "src/utils.go",
		StartLine:     5,
		EndLine:       15,
		IsExported:    true,
		Content:       "func helper() string { ... }",
		Signature:     "func helper() string",
		Description:   "A utility helper function",
	})

	graph.AddEdge(g, mainFile, mainFn, graph.RelContains, nil)
	graph.AddEdge(g, utilsFile, helperFn, graph.RelContains, nil)

	graph.AddEdge(g, mainFn, helperFn, graph.RelCalls, map[string]any{
		graph.PropConfidence: 0.95,
		graph.PropReason:     "direct call",
	})

	svcClass := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Service", Name: "Service"},
		FilePath:      "src/main.go",
		StartLine:     30,
		EndLine:       60,
		IsExported:    true,
	})
	runMethod := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Service.Run", Name: "Run"},
		FilePath:      "src/main.go",
		StartLine:     35,
		EndLine:       55,
		IsExported:    true,
		Signature:     "func (s *Service) Run() error",
	})

	graph.AddEdge(g, mainFile, svcClass, graph.RelContains, nil)
	graph.AddEdge(g, svcClass, runMethod, graph.RelHasMethod, nil)

	graph.AddEdge(g, mainFn, runMethod, graph.RelCalls, map[string]any{
		graph.PropConfidence: 0.90,
	})

	community := graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "community:0", Name: "core"},
		Modularity:    0.45,
		Size:          3,
	})
	graph.AddEdge(g, mainFn, community, graph.RelMemberOf, nil)
	graph.AddEdge(g, helperFn, community, graph.RelMemberOf, nil)
	graph.AddEdge(g, runMethod, community, graph.RelMemberOf, nil)

	process := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps:  graph.BaseNodeProps{ID: "process:main-flow", Name: "main-flow"},
		EntryPoint:     "func:main",
		HeuristicLabel: "Application entry point",
		StepCount:      3,
	})
	graph.AddTypedEdge(g, mainFn, process, graph.EdgeProps{
		Type: graph.RelStepInProcess,
		Step: 1,
	})
	graph.AddTypedEdge(g, helperFn, process, graph.EdgeProps{
		Type: graph.RelStepInProcess,
		Step: 2,
	})
	graph.AddTypedEdge(g, runMethod, process, graph.EdgeProps{
		Type: graph.RelStepInProcess,
		Step: 3,
	})

	return g
}

// MustFindNode finds a node by ID or panics. For use in tests only.
func MustFindNode(g *lpg.Graph, id string) *lpg.Node {
	node := graph.FindNodeByID(g, id)
	if node == nil {
		panic(fmt.Sprintf("testutil.MustFindNode: node %q not found", id))
	}
	return node
}

// TempDir creates a temporary directory with the given structure and returns
// its path. The directory is automatically cleaned up when the test finishes.
// Structure is a map of relative paths to file contents. Paths ending in "/"
// create empty directories.
func TempDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if relPath[len(relPath)-1] == '/' {
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				t.Fatalf("TempDir: mkdir %s: %v", relPath, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("TempDir: mkdir parent of %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("TempDir: write %s: %v", relPath, err)
		}
	}
	return dir
}

// AssertNodeCount checks that the graph has exactly the expected number of nodes.
func AssertNodeCount(t *testing.T, g *lpg.Graph, expected int) {
	t.Helper()
	got := graph.NodeCount(g)
	if got != expected {
		t.Errorf("expected %d nodes, got %d", expected, got)
	}
}

// AssertEdgeCount checks that the graph has exactly the expected number of edges.
func AssertEdgeCount(t *testing.T, g *lpg.Graph, expected int) {
	t.Helper()
	got := graph.EdgeCount(g)
	if got != expected {
		t.Errorf("expected %d edges, got %d", expected, got)
	}
}

// AssertHasNode checks that a node with the given ID exists in the graph.
func AssertHasNode(t *testing.T, g *lpg.Graph, id string) {
	t.Helper()
	if graph.FindNodeByID(g, id) == nil {
		t.Errorf("expected node %q to exist", id)
	}
}

// AssertHasNoNode checks that a node with the given ID does NOT exist in the graph.
func AssertHasNoNode(t *testing.T, g *lpg.Graph, id string) {
	t.Helper()
	if graph.FindNodeByID(g, id) != nil {
		t.Errorf("expected node %q to NOT exist", id)
	}
}

// AssertLabelCount checks the number of nodes with a specific label.
func AssertLabelCount(t *testing.T, g *lpg.Graph, label graph.NodeLabel, expected int) {
	t.Helper()
	nodes := graph.FindNodesByLabel(g, label)
	if len(nodes) != expected {
		t.Errorf("expected %d %s nodes, got %d", expected, label, len(nodes))
	}
}

// AssertHasEdge checks that an edge exists from→to with the given rel type.
func AssertHasEdge(t *testing.T, fromNode *lpg.Node, toID string, relType graph.RelType) {
	t.Helper()
	for _, edge := range graph.GetOutgoingEdges(fromNode, relType) {
		if graph.GetStringProp(edge.GetTo(), graph.PropID) == toID {
			return
		}
	}
	fromID := graph.GetStringProp(fromNode, graph.PropID)
	t.Errorf("expected %s edge from %q to %q", relType, fromID, toID)
}
