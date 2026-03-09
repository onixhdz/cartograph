package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/testutil"
)

const testLangPython = "python"

func TestProcessStructure_FlatDirectory(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "src", IsDir: true},
		{RelPath: "src/main.go", IsDir: false, Size: 100, Language: "go"},
		{RelPath: "src/utils.go", IsDir: false, Size: 200, Language: "go"},
		{RelPath: "src/types.go", IsDir: false, Size: 150, Language: "go"},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure: %v", err)
	}

	// 1 folder + 3 files = 4 nodes.
	testutil.AssertNodeCount(t, g, 4)
	testutil.AssertLabelCount(t, g, graph.LabelFolder, 1)
	testutil.AssertLabelCount(t, g, graph.LabelFile, 3)

	// 3 CONTAINS edges: folder -> each file.
	testutil.AssertEdgeCount(t, g, 3)
}

func TestProcessStructure_NestedDirectories(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "src", IsDir: true},
		{RelPath: "src/pkg", IsDir: true},
		{RelPath: "src/pkg/file.go", IsDir: false, Size: 100, Language: "go"},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure: %v", err)
	}

	// 2 folders + 1 file = 3 nodes.
	testutil.AssertNodeCount(t, g, 3)
	testutil.AssertHasNode(t, g, "folder:src")
	testutil.AssertHasNode(t, g, "folder:src/pkg")
	testutil.AssertHasNode(t, g, "file:src/pkg/file.go")

	// src -> src/pkg (CONTAINS) + src/pkg -> file.go (CONTAINS) = 2 edges.
	testutil.AssertEdgeCount(t, g, 2)
}

func TestProcessStructure_CorrectLabels(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "lib", IsDir: true},
		{RelPath: "lib/code.ts", IsDir: false, Size: 50, Language: "typescript"},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure: %v", err)
	}

	folderNode := graph.FindNodeByID(g, "folder:lib")
	if folderNode == nil {
		t.Fatal("folder node not found")
		return
	}
	if !folderNode.HasLabel(string(graph.LabelFolder)) {
		t.Error("folder node should have Folder label")
	}

	fileNode := graph.FindNodeByID(g, "file:lib/code.ts")
	if fileNode == nil {
		t.Fatal("file node not found")
		return
	}
	if !fileNode.HasLabel(string(graph.LabelFile)) {
		t.Error("file node should have File label")
	}
}

func TestProcessStructure_FileProperties(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "src", IsDir: true},
		{RelPath: "src/app.py", IsDir: false, Size: 2048, Language: testLangPython},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure: %v", err)
	}

	node := graph.FindNodeByID(g, "file:src/app.py")
	if node == nil {
		t.Fatal("file node not found")
		return
	}

	if got := graph.GetStringProp(node, graph.PropFilePath); got != "src/app.py" {
		t.Errorf("filePath: expected %q, got %q", "src/app.py", got)
	}
	if got := graph.GetStringProp(node, graph.PropLanguage); got != testLangPython {
		t.Errorf("language: expected %q, got %q", testLangPython, got)
	}
}

func TestProcessStructure_FolderProperties(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "pkg", IsDir: true},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure: %v", err)
	}

	node := graph.FindNodeByID(g, "folder:pkg")
	if node == nil {
		t.Fatal("folder node not found")
		return
	}
	if got := graph.GetStringProp(node, graph.PropFilePath); got != "pkg" {
		t.Errorf("filePath: expected %q, got %q", "pkg", got)
	}
}

func TestProcessStructure_NoDuplicates(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "src", IsDir: true},
		{RelPath: "src/a.go", IsDir: false, Size: 10, Language: "go"},
		{RelPath: "src/b.go", IsDir: false, Size: 20, Language: "go"},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure (1st): %v", err)
	}
	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure (2nd): %v", err)
	}

	// Should still have 3 nodes (1 folder + 2 files), not 6.
	testutil.AssertNodeCount(t, g, 3)
}

func TestProcessStructure_IntermediateDirectories(t *testing.T) {
	g := lpg.NewGraph()
	results := []WalkResult{
		{RelPath: "a/b/c/file.go", IsDir: false, Size: 10, Language: "go"},
	}

	if err := ProcessStructure(g, results); err != nil {
		t.Fatalf("ProcessStructure: %v", err)
	}

	testutil.AssertHasNode(t, g, "folder:a")
	testutil.AssertHasNode(t, g, "folder:a/b")
	testutil.AssertHasNode(t, g, "folder:a/b/c")
	testutil.AssertHasNode(t, g, "file:a/b/c/file.go")

	// 3 folders + 1 file = 4 nodes.
	testutil.AssertNodeCount(t, g, 4)
}
