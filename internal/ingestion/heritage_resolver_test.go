package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

func setupHeritageGraph() *lpg.Graph {
	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Animal", Name: "Animal"},
		FilePath:      "src/animal.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Dog", Name: "Dog"},
		FilePath:      "src/dog.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Runnable", Name: "Runnable"},
		FilePath:      "src/runnable.go",
		StartLine:     1,
		EndLine:       5,
	})
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Cat", Name: "Cat"},
		FilePath:      "src/cat.go",
		StartLine:     1,
		EndLine:       8,
	})
	return g
}

func TestResolveHeritage_Extends(t *testing.T) {
	g := setupHeritageGraph()

	relations := []HeritageInfo{
		{ChildNodeID: "class:Dog", ParentName: "Animal", Kind: "extends"},
	}

	count := ResolveHeritage(g, relations)
	if count != 1 {
		t.Errorf("expected 1 resolved, got %d", count)
	}

	dogNode := graph.FindNodeByID(g, "class:Dog")
	edges := graph.GetOutgoingEdges(dogNode, graph.RelExtends)
	if len(edges) != 1 {
		t.Fatalf("expected 1 EXTENDS edge, got %d", len(edges))
	}
	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "class:Animal" {
		t.Errorf("expected target class:Animal, got %s", targetID)
	}
}

func TestResolveHeritage_Implements(t *testing.T) {
	g := setupHeritageGraph()

	relations := []HeritageInfo{
		{ChildNodeID: "class:Dog", ParentName: "Runnable", Kind: "implements"},
	}

	count := ResolveHeritage(g, relations)
	if count != 1 {
		t.Errorf("expected 1 resolved, got %d", count)
	}

	dogNode := graph.FindNodeByID(g, "class:Dog")
	edges := graph.GetOutgoingEdges(dogNode, graph.RelImplements)
	if len(edges) != 1 {
		t.Fatalf("expected 1 IMPLEMENTS edge, got %d", len(edges))
	}
	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "iface:Runnable" {
		t.Errorf("expected target iface:Runnable, got %s", targetID)
	}
}

func TestResolveHeritage_MissingParent(t *testing.T) {
	g := setupHeritageGraph()

	relations := []HeritageInfo{
		{ChildNodeID: "class:Dog", ParentName: "NonExistent", Kind: "extends"},
	}

	count := ResolveHeritage(g, relations)
	if count != 0 {
		t.Errorf("expected 0 resolved, got %d", count)
	}

	dogNode := graph.FindNodeByID(g, "class:Dog")
	edges := graph.GetOutgoingEdges(dogNode, graph.RelExtends)
	if len(edges) != 0 {
		t.Errorf("expected 0 EXTENDS edges, got %d", len(edges))
	}
}

func TestResolveHeritage_MissingChild(t *testing.T) {
	g := setupHeritageGraph()

	relations := []HeritageInfo{
		{ChildNodeID: "class:NonExistent", ParentName: "Animal", Kind: "extends"},
	}

	count := ResolveHeritage(g, relations)
	if count != 0 {
		t.Errorf("expected 0 resolved, got %d", count)
	}
}

func TestResolveHeritage_MixedExtendsAndImplements(t *testing.T) {
	g := setupHeritageGraph()

	relations := []HeritageInfo{
		{ChildNodeID: "class:Dog", ParentName: "Animal", Kind: "extends"},
		{ChildNodeID: "class:Dog", ParentName: "Runnable", Kind: "implements"},
		{ChildNodeID: "class:Cat", ParentName: "Animal", Kind: "extends"},
	}

	count := ResolveHeritage(g, relations)
	if count != 3 {
		t.Errorf("expected 3 resolved, got %d", count)
	}

	dogNode := graph.FindNodeByID(g, "class:Dog")
	extendsEdges := graph.GetOutgoingEdges(dogNode, graph.RelExtends)
	if len(extendsEdges) != 1 {
		t.Errorf("expected 1 EXTENDS edge from Dog, got %d", len(extendsEdges))
	}
	implEdges := graph.GetOutgoingEdges(dogNode, graph.RelImplements)
	if len(implEdges) != 1 {
		t.Errorf("expected 1 IMPLEMENTS edge from Dog, got %d", len(implEdges))
	}

	catNode := graph.FindNodeByID(g, "class:Cat")
	catExtends := graph.GetOutgoingEdges(catNode, graph.RelExtends)
	if len(catExtends) != 1 {
		t.Errorf("expected 1 EXTENDS edge from Cat, got %d", len(catExtends))
	}
}

func TestResolveHeritage_ReturnsCorrectCount(t *testing.T) {
	g := setupHeritageGraph()

	relations := []HeritageInfo{
		{ChildNodeID: "class:Dog", ParentName: "Animal", Kind: "extends"},
		{ChildNodeID: "class:Dog", ParentName: "NonExistent", Kind: "extends"},
		{ChildNodeID: "class:Cat", ParentName: "Runnable", Kind: "implements"},
		{ChildNodeID: "class:Missing", ParentName: "Animal", Kind: "extends"},
	}

	count := ResolveHeritage(g, relations)
	if count != 2 {
		t.Errorf("expected 2 resolved (2 skipped), got %d", count)
	}
}

// --- Structural interface resolution tests ---

func TestResolveStructuralInterfaces_MethodSetMatch(t *testing.T) {
	g := lpg.NewGraph()

	// Interface with 2 methods.
	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Writer", Name: "Writer"},
		FilePath:      "io.go",
	})
	writeMethod := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Writer.Write", Name: "Write"},
		FilePath:      "io.go",
	})
	closeMethod := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Writer.Close", Name: "Close"},
		FilePath:      "io.go",
	})
	graph.AddEdge(g, iface, writeMethod, graph.RelHasMethod, nil)
	graph.AddEdge(g, iface, closeMethod, graph.RelHasMethod, nil)

	// Struct with matching methods (superset).
	impl := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:FileWriter", Name: "FileWriter"},
		FilePath:      "file.go",
	})
	fw1 := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:FileWriter.Write", Name: "Write"},
		FilePath:      "file.go",
	})
	fw2 := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:FileWriter.Close", Name: "Close"},
		FilePath:      "file.go",
	})
	fw3 := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:FileWriter.Flush", Name: "Flush"},
		FilePath:      "file.go",
	})
	graph.AddEdge(g, impl, fw1, graph.RelHasMethod, nil)
	graph.AddEdge(g, impl, fw2, graph.RelHasMethod, nil)
	graph.AddEdge(g, impl, fw3, graph.RelHasMethod, nil)

	count := ResolveStructuralInterfaces(g)
	if count != 1 {
		t.Fatalf("expected 1 IMPLEMENTS edge created, got %d", count)
	}

	edges := graph.GetOutgoingEdges(impl, graph.RelImplements)
	if len(edges) != 1 {
		t.Fatalf("expected 1 IMPLEMENTS edge from FileWriter, got %d", len(edges))
	}
	target := graph.GetStringProp(edges[0].GetTo(), graph.PropName)
	if target != "Writer" {
		t.Errorf("expected IMPLEMENTS edge to Writer, got %s", target)
	}
}

func TestResolveStructuralInterfaces_PartialMatch(t *testing.T) {
	g := lpg.NewGraph()

	// Interface with 2 methods.
	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:ReadWriter", Name: "ReadWriter"},
		FilePath:      "io.go",
	})
	m1 := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:ReadWriter.Read", Name: "Read"},
		FilePath:      "io.go",
	})
	m2 := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:ReadWriter.Write", Name: "Write"},
		FilePath:      "io.go",
	})
	graph.AddEdge(g, iface, m1, graph.RelHasMethod, nil)
	graph.AddEdge(g, iface, m2, graph.RelHasMethod, nil)

	// Struct with only 1 of the 2 methods — should NOT satisfy.
	partial := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:Reader", Name: "Reader"},
		FilePath:      "read.go",
	})
	pm := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Reader.Read", Name: "Read"},
		FilePath:      "read.go",
	})
	graph.AddEdge(g, partial, pm, graph.RelHasMethod, nil)

	count := ResolveStructuralInterfaces(g)
	if count != 0 {
		t.Errorf("expected 0 IMPLEMENTS edges (partial match), got %d", count)
	}
}

func TestResolveStructuralInterfaces_SkipsExistingEdge(t *testing.T) {
	g := lpg.NewGraph()

	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Saver", Name: "Saver"},
		FilePath:      "saver.go",
	})
	saveSpec := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Saver.Save", Name: "Save"},
		FilePath:      "saver.go",
	})
	graph.AddEdge(g, iface, saveSpec, graph.RelHasMethod, nil)

	impl := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:DB", Name: "DB"},
		FilePath:      "db.go",
	})
	saveImpl := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:DB.Save", Name: "Save"},
		FilePath:      "db.go",
	})
	graph.AddEdge(g, impl, saveImpl, graph.RelHasMethod, nil)

	// Add explicit IMPLEMENTS edge first.
	graph.AddEdge(g, impl, iface, graph.RelImplements, nil)

	// Structural resolution should not create a duplicate.
	count := ResolveStructuralInterfaces(g)
	if count != 0 {
		t.Errorf("expected 0 new edges (already exists), got %d", count)
	}

	edges := graph.GetOutgoingEdges(impl, graph.RelImplements)
	if len(edges) != 1 {
		t.Errorf("expected exactly 1 IMPLEMENTS edge (no duplicate), got %d", len(edges))
	}
}

func TestResolveStructuralInterfaces_NoMethodsNoEdge(t *testing.T) {
	g := lpg.NewGraph()

	// Interface with no HAS_METHOD edges (empty or methods not extracted).
	graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Empty", Name: "Empty"},
		FilePath:      "empty.go",
	})
	impl := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:S", Name: "S"},
		FilePath:      "s.go",
	})
	m := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:S.Foo", Name: "Foo"},
		FilePath:      "s.go",
	})
	graph.AddEdge(g, impl, m, graph.RelHasMethod, nil)

	count := ResolveStructuralInterfaces(g)
	if count != 0 {
		t.Errorf("expected 0 edges (interface has no methods), got %d", count)
	}
}

func TestResolveHeritage_TraitKind(t *testing.T) {
	g := setupHeritageGraph()

	// Add a trait parent node (e.g., Rust trait or Ruby module).
	graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "trait:Display", Name: "Display"},
		FilePath:      "display.rs",
	})

	relations := []HeritageInfo{
		{ChildNodeID: "class:Cat", ParentName: "Display", Kind: "trait"},
	}

	count := ResolveHeritage(g, relations)
	if count != 1 {
		t.Errorf("expected 1 resolved trait relation, got %d", count)
	}

	// Verify it created an IMPLEMENTS edge.
	cat := graph.FindNodeByID(g, "class:Cat")
	if cat == nil {
		t.Fatal("Cat node not found")
		return
	}
	implEdges := graph.GetOutgoingEdges(cat, graph.RelImplements)
	found := false
	for _, e := range implEdges {
		to := e.GetTo()
		if graph.GetStringProp(to, graph.PropName) == "Display" {
			found = true
		}
	}
	if !found {
		t.Error("expected IMPLEMENTS edge from Cat to Display")
	}
}
