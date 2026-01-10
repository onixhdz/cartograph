package graph

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
)

func TestAddNode(t *testing.T) {
	g := lpg.NewGraph()
	node := AddNode(g, LabelFunction, map[string]any{
		PropID:   "func:foo",
		PropName: "foo",
	})
	if node == nil {
		t.Fatal("AddNode returned nil")
	}
	if !node.HasLabel(string(LabelFunction)) {
		t.Errorf("expected label %q", LabelFunction)
	}
	v, ok := node.GetProperty(PropID)
	if !ok || v != "func:foo" {
		t.Errorf("expected id %q, got %v", "func:foo", v)
	}
}

func TestAddFileNode(t *testing.T) {
	g := lpg.NewGraph()
	node := AddFileNode(g, FileProps{
		BaseNodeProps: BaseNodeProps{ID: "file:main.go", Name: "main.go"},
		FilePath:      "src/main.go",
		Language:      "go",
		Size:          2048,
		Content:       "package main",
	})
	if !node.HasLabel(string(LabelFile)) {
		t.Errorf("expected label File")
	}
	if GetStringProp(node, PropFilePath) != "src/main.go" {
		t.Errorf("filePath mismatch")
	}
	if GetStringProp(node, PropLanguage) != "go" {
		t.Errorf("language mismatch")
	}
	if GetIntProp(node, PropSize) != 2048 {
		t.Errorf("size mismatch")
	}
}

func TestAddFolderNode(t *testing.T) {
	g := lpg.NewGraph()
	node := AddFolderNode(g, FolderProps{
		BaseNodeProps: BaseNodeProps{ID: "folder:src", Name: "src"},
		FilePath:      "src/",
	})
	if !node.HasLabel(string(LabelFolder)) {
		t.Errorf("expected label Folder")
	}
	if GetStringProp(node, PropFilePath) != "src/" {
		t.Errorf("filePath mismatch")
	}
}

func TestAddSymbolNode(t *testing.T) {
	g := lpg.NewGraph()
	node := AddSymbolNode(g, LabelMethod, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "method:Foo.Bar", Name: "Bar"},
		FilePath:      "pkg/foo.go",
		StartLine:     10,
		EndLine:       20,
		IsExported:    true,
		Content:       "func (f *Foo) Bar() {}",
		Description:   "Bar method",
		Signature:     "func (f *Foo) Bar()",
	})
	if !node.HasLabel(string(LabelMethod)) {
		t.Errorf("expected label Method")
	}
	if GetStringProp(node, PropName) != "Bar" {
		t.Errorf("name mismatch")
	}
	if GetIntProp(node, PropStartLine) != 10 {
		t.Errorf("startLine mismatch")
	}
	if GetIntProp(node, PropEndLine) != 20 {
		t.Errorf("endLine mismatch")
	}
	if !GetBoolProp(node, PropIsExported) {
		t.Errorf("expected isExported=true")
	}
	if GetStringProp(node, PropSignature) != "func (f *Foo) Bar()" {
		t.Errorf("signature mismatch")
	}
}

func TestAddCommunityNode(t *testing.T) {
	g := lpg.NewGraph()
	node := AddCommunityNode(g, CommunityProps{
		BaseNodeProps: BaseNodeProps{ID: "community:1", Name: "auth"},
		Modularity:    0.72,
		Size:          5,
	})
	if !node.HasLabel(string(LabelCommunity)) {
		t.Errorf("expected label Community")
	}
	v, ok := node.GetProperty(PropModularity)
	if !ok {
		t.Fatal("missing modularity")
	}
	if fv, ok := v.(float64); !ok || fv != 0.72 {
		t.Errorf("modularity mismatch: %v", v)
	}
	if GetIntProp(node, PropCommunitySize) != 5 {
		t.Errorf("size mismatch")
	}
}

func TestAddProcessNode(t *testing.T) {
	g := lpg.NewGraph()
	node := AddProcessNode(g, ProcessProps{
		BaseNodeProps:  BaseNodeProps{ID: "process:auth", Name: "auth-flow"},
		EntryPoint:     "func:login",
		HeuristicLabel: "Authentication flow",
		StepCount:      4,
	})
	if !node.HasLabel(string(LabelProcess)) {
		t.Errorf("expected label Process")
	}
	if GetStringProp(node, PropEntryPoint) != "func:login" {
		t.Errorf("entryPoint mismatch")
	}
	if GetStringProp(node, PropHeuristicLabel) != "Authentication flow" {
		t.Errorf("heuristicLabel mismatch")
	}
	if GetIntProp(node, PropStepCount) != 4 {
		t.Errorf("stepCount mismatch")
	}
}

func TestAddEdge(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a", PropName: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b", PropName: "b"})

	edge := AddEdge(g, a, b, RelCalls, map[string]any{
		PropConfidence: 0.85,
		PropReason:     "direct",
	})
	if edge == nil {
		t.Fatal("AddEdge returned nil")
	}
	if edge.GetLabel() != string(RelCalls) {
		t.Errorf("expected edge label %q, got %q", RelCalls, edge.GetLabel())
	}
	rt, err := GetEdgeRelType(edge)
	if err != nil {
		t.Fatalf("GetEdgeRelType error: %v", err)
	}
	if rt != RelCalls {
		t.Errorf("expected rel type %q, got %q", RelCalls, rt)
	}
	v, ok := edge.GetProperty(PropConfidence)
	fv, typeOk := v.(float64)
	if !ok || !typeOk || fv != 0.85 {
		t.Errorf("confidence mismatch")
	}
}

func TestAddEdgeNilProps(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFile, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFolder, map[string]any{PropID: "b"})

	edge := AddEdge(g, a, b, RelContains, nil)
	rt, err := GetEdgeRelType(edge)
	if err != nil {
		t.Fatalf("GetEdgeRelType error: %v", err)
	}
	if rt != RelContains {
		t.Errorf("expected CONTAINS, got %q", rt)
	}
}

func TestAddTypedEdge(t *testing.T) {
	g := lpg.NewGraph()
	fn := AddNode(g, LabelFunction, map[string]any{PropID: "fn"})
	proc := AddNode(g, LabelProcess, map[string]any{PropID: "proc"})

	edge := AddTypedEdge(g, fn, proc, EdgeProps{
		Type: RelStepInProcess,
		Step: 3,
	})
	rt, _ := GetEdgeRelType(edge)
	if rt != RelStepInProcess {
		t.Errorf("expected STEP_IN_PROCESS, got %q", rt)
	}
	v, ok := edge.GetProperty(PropStep)
	iv, typeOk := v.(int)
	if !ok || !typeOk || iv != 3 {
		t.Errorf("step mismatch: %v", v)
	}
}

func TestFindNodeByID(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFunction, map[string]any{PropID: "func:a", PropName: "a"})
	AddNode(g, LabelFunction, map[string]any{PropID: "func:b", PropName: "b"})

	node := FindNodeByID(g, "func:b")
	if node == nil {
		t.Fatal("FindNodeByID returned nil for existing node")
	}
	if GetStringProp(node, PropName) != "b" {
		t.Errorf("expected name 'b'")
	}

	missing := FindNodeByID(g, "func:nonexistent")
	if missing != nil {
		t.Error("FindNodeByID should return nil for missing node")
	}
}

func TestFindNodesByLabel(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFunction, map[string]any{PropID: "func:a"})
	AddNode(g, LabelFunction, map[string]any{PropID: "func:b"})
	AddNode(g, LabelClass, map[string]any{PropID: "class:c"})

	fns := FindNodesByLabel(g, LabelFunction)
	if len(fns) != 2 {
		t.Errorf("expected 2 Function nodes, got %d", len(fns))
	}

	classes := FindNodesByLabel(g, LabelClass)
	if len(classes) != 1 {
		t.Errorf("expected 1 Class node, got %d", len(classes))
	}

	ifaces := FindNodesByLabel(g, LabelInterface)
	if len(ifaces) != 0 {
		t.Errorf("expected 0 Interface nodes, got %d", len(ifaces))
	}
}

func TestGetEdgeRelTypeErrors(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})

	// Edge with a non-empty label but no type property — label is used as rel type.
	edge := g.NewEdge(a, b, "raw", nil)
	rt, err := GetEdgeRelType(edge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt != "raw" {
		t.Errorf("expected rel type %q from label, got %q", "raw", rt)
	}

	// Edge with empty label and no type property — should error.
	edge2 := g.NewEdge(a, b, "", nil)
	_, err = GetEdgeRelType(edge2)
	if err == nil {
		t.Error("expected error for edge with empty label and no type property")
	}
}

func TestGetOutgoingEdges(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})
	c := AddNode(g, LabelClass, map[string]any{PropID: "c"})

	AddEdge(g, a, b, RelCalls, nil)
	AddEdge(g, a, c, RelImports, nil)

	all := GetOutgoingEdges(a, "")
	if len(all) != 2 {
		t.Errorf("expected 2 outgoing edges, got %d", len(all))
	}

	calls := GetOutgoingEdges(a, RelCalls)
	if len(calls) != 1 {
		t.Errorf("expected 1 CALLS edge, got %d", len(calls))
	}

	imports := GetOutgoingEdges(a, RelImports)
	if len(imports) != 1 {
		t.Errorf("expected 1 IMPORTS edge, got %d", len(imports))
	}

	none := GetOutgoingEdges(b, RelCalls)
	if len(none) != 0 {
		t.Errorf("expected 0 outgoing CALLS from b, got %d", len(none))
	}
}

func TestGetIncomingEdges(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})

	AddEdge(g, a, b, RelCalls, nil)

	incoming := GetIncomingEdges(b, RelCalls)
	if len(incoming) != 1 {
		t.Errorf("expected 1 incoming CALLS edge, got %d", len(incoming))
	}

	noIncoming := GetIncomingEdges(a, RelCalls)
	if len(noIncoming) != 0 {
		t.Errorf("expected 0 incoming CALLS to a, got %d", len(noIncoming))
	}
}

func TestNodeCountEdgeCount(t *testing.T) {
	g := lpg.NewGraph()
	if NodeCount(g) != 0 {
		t.Errorf("empty graph should have 0 nodes")
	}
	if EdgeCount(g) != 0 {
		t.Errorf("empty graph should have 0 edges")
	}

	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})
	AddEdge(g, a, b, RelCalls, nil)

	if NodeCount(g) != 2 {
		t.Errorf("expected 2 nodes, got %d", NodeCount(g))
	}
	if EdgeCount(g) != 1 {
		t.Errorf("expected 1 edge, got %d", EdgeCount(g))
	}
}

func TestGetStringProp(t *testing.T) {
	g := lpg.NewGraph()
	node := AddNode(g, LabelFunction, map[string]any{
		PropID:   "func:x",
		PropName: "x",
	})
	if GetStringProp(node, PropName) != "x" {
		t.Error("expected 'x'")
	}
	if GetStringProp(node, PropDescription) != "" {
		t.Error("missing prop should return empty string")
	}
}

func TestGetIntProp(t *testing.T) {
	g := lpg.NewGraph()
	node := AddNode(g, LabelFunction, map[string]any{
		PropID:        "func:x",
		PropStartLine: 42,
	})
	if GetIntProp(node, PropStartLine) != 42 {
		t.Error("expected 42")
	}
	if GetIntProp(node, PropEndLine) != 0 {
		t.Error("missing prop should return 0")
	}

	// Test int64 coercion
	node2 := AddNode(g, LabelFunction, map[string]any{
		PropID:        "func:y",
		PropStartLine: int64(99),
	})
	if GetIntProp(node2, PropStartLine) != 99 {
		t.Error("expected int64 coercion to 99")
	}

	// Test float64 coercion
	node3 := AddNode(g, LabelFunction, map[string]any{
		PropID:        "func:z",
		PropStartLine: float64(77),
	})
	if GetIntProp(node3, PropStartLine) != 77 {
		t.Error("expected float64 coercion to 77")
	}
}

func TestGetBoolProp(t *testing.T) {
	g := lpg.NewGraph()
	node := AddNode(g, LabelFunction, map[string]any{
		PropID:         "func:x",
		PropIsExported: true,
	})
	if !GetBoolProp(node, PropIsExported) {
		t.Error("expected true")
	}
	if GetBoolProp(node, PropDescription) {
		t.Error("missing prop should return false")
	}
}

func TestFindNodesByName(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFunction, map[string]any{PropID: "a", PropName: "helper"})
	AddNode(g, LabelMethod, map[string]any{PropID: "b", PropName: "helper"})
	AddNode(g, LabelClass, map[string]any{PropID: "c", PropName: "Service"})

	results := FindNodesByName(g, "helper")
	if len(results) != 2 {
		t.Errorf("expected 2 nodes named 'helper', got %d", len(results))
	}

	results2 := FindNodesByName(g, "nonexistent")
	if len(results2) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(results2))
	}
}

func TestFindNodesByNameAndLabel(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFunction, map[string]any{PropID: "a", PropName: "Run"})
	AddNode(g, LabelMethod, map[string]any{PropID: "b", PropName: "Run"})

	fns := FindNodesByNameAndLabel(g, "Run", LabelFunction)
	if len(fns) != 1 {
		t.Errorf("expected 1 Function named 'Run', got %d", len(fns))
	}

	methods := FindNodesByNameAndLabel(g, "Run", LabelMethod)
	if len(methods) != 1 {
		t.Errorf("expected 1 Method named 'Run', got %d", len(methods))
	}
}

func TestFindNodeByFilePath(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFile, map[string]any{PropID: "f1", PropFilePath: "src/main.go"})
	AddNode(g, LabelFile, map[string]any{PropID: "f2", PropFilePath: "src/utils.go"})

	node := FindNodeByFilePath(g, "src/utils.go")
	if node == nil {
		t.Fatal("expected to find file node")
	}
	if GetStringProp(node, PropID) != "f2" {
		t.Errorf("expected f2, got %s", GetStringProp(node, PropID))
	}

	missing := FindNodeByFilePath(g, "nonexistent.go")
	if missing != nil {
		t.Error("expected nil for missing file")
	}
}

func TestNodeLabelsAndRelTypes(t *testing.T) {
	// Verify all labels and rel types are non-empty and unique
	seen := make(map[string]bool)
	for _, l := range AllNodeLabels {
		if l == "" {
			t.Error("empty NodeLabel found")
		}
		if seen[string(l)] {
			t.Errorf("duplicate NodeLabel: %s", l)
		}
		seen[string(l)] = true
	}

	seenRel := make(map[string]bool)
	for _, r := range AllRelTypes {
		if r == "" {
			t.Error("empty RelType found")
		}
		if seenRel[string(r)] {
			t.Errorf("duplicate RelType: %s", r)
		}
		seenRel[string(r)] = true
	}
}

func TestGetFloat64Prop(t *testing.T) {
	g := lpg.NewGraph()
	node := AddNode(g, LabelCommunity, map[string]any{
		PropID:         "c:1",
		PropModularity: 0.73,
	})
	if GetFloat64Prop(node, PropModularity) != 0.73 {
		t.Errorf("expected 0.73, got %f", GetFloat64Prop(node, PropModularity))
	}
	if GetFloat64Prop(node, PropConfidence) != 0 {
		t.Error("missing prop should return 0")
	}

	// int coercion
	node2 := AddNode(g, LabelCommunity, map[string]any{
		PropID:         "c:2",
		PropModularity: 1,
	})
	if GetFloat64Prop(node2, PropModularity) != 1.0 {
		t.Errorf("int coercion failed")
	}

	// int64 coercion
	node3 := AddNode(g, LabelCommunity, map[string]any{
		PropID:         "c:3",
		PropModularity: int64(2),
	})
	if GetFloat64Prop(node3, PropModularity) != 2.0 {
		t.Errorf("int64 coercion failed")
	}
}

func TestGetNeighbors(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a", PropName: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b", PropName: "b"})
	c := AddNode(g, LabelFunction, map[string]any{PropID: "c", PropName: "c"})

	AddEdge(g, a, b, RelCalls, nil)
	AddEdge(g, a, c, RelCalls, nil)
	AddEdge(g, b, c, RelImports, nil)

	callees := GetNeighbors(a, lpg.OutgoingEdge, RelCalls)
	if len(callees) != 2 {
		t.Errorf("expected 2 outgoing CALLS neighbors, got %d", len(callees))
	}

	callers := GetNeighbors(b, lpg.IncomingEdge, RelCalls)
	if len(callers) != 1 {
		t.Errorf("expected 1 incoming CALLS neighbor, got %d", len(callers))
	}

	allOut := GetNeighbors(a, lpg.OutgoingEdge, "")
	if len(allOut) != 2 {
		t.Errorf("expected 2 outgoing neighbors (all types), got %d", len(allOut))
	}

	bImports := GetNeighbors(b, lpg.OutgoingEdge, RelImports)
	if len(bImports) != 1 {
		t.Errorf("expected 1 outgoing IMPORTS neighbor from b, got %d", len(bImports))
	}

	noNeighbors := GetNeighbors(c, lpg.OutgoingEdge, RelCalls)
	if len(noNeighbors) != 0 {
		t.Errorf("expected 0 outgoing CALLS from c, got %d", len(noNeighbors))
	}
}

func TestNodeToMap(t *testing.T) {
	g := lpg.NewGraph()
	node := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "src/main.go",
		StartLine:     1,
		EndLine:       10,
	})

	m := NodeToMap(node)
	if m["id"] != "func:main" {
		t.Errorf("id mismatch: %q", m["id"])
	}
	if m["name"] != "main" {
		t.Errorf("name mismatch: %q", m["name"])
	}
	if m["filePath"] != "src/main.go" {
		t.Errorf("filePath mismatch: %q", m["filePath"])
	}
	if m["label"] != "Function" {
		t.Errorf("label mismatch: %q", m["label"])
	}
}

func TestForEachNode(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	AddNode(g, LabelFunction, map[string]any{PropID: "b"})
	AddNode(g, LabelFunction, map[string]any{PropID: "c"})

	// Count all
	count := 0
	ForEachNode(g, func(n *lpg.Node) bool {
		count++
		return true
	})
	if count != 3 {
		t.Errorf("expected 3 nodes, visited %d", count)
	}

	// Early termination
	count = 0
	ForEachNode(g, func(n *lpg.Node) bool {
		count++
		return count < 2
	})
	if count != 2 {
		t.Errorf("expected early termination after 2, visited %d", count)
	}
}

func TestForEachEdge(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b"})
	c := AddNode(g, LabelFunction, map[string]any{PropID: "c"})
	AddEdge(g, a, b, RelCalls, nil)
	AddEdge(g, b, c, RelCalls, nil)

	count := 0
	ForEachEdge(g, func(e *lpg.Edge) bool {
		count++
		return true
	})
	if count != 2 {
		t.Errorf("expected 2 edges, visited %d", count)
	}

	// Early termination
	count = 0
	ForEachEdge(g, func(e *lpg.Edge) bool {
		count++
		return false
	})
	if count != 1 {
		t.Errorf("expected early termination after 1, visited %d", count)
	}
}

func TestRemoveNode(t *testing.T) {
	g := lpg.NewGraph()
	a := AddNode(g, LabelFunction, map[string]any{PropID: "a", PropName: "A"})
	b := AddNode(g, LabelFunction, map[string]any{PropID: "b", PropName: "B"})
	AddEdge(g, a, b, RelCalls, nil)

	if NodeCount(g) != 2 {
		t.Fatalf("expected 2 nodes before remove, got %d", NodeCount(g))
	}
	if EdgeCount(g) != 1 {
		t.Fatalf("expected 1 edge before remove, got %d", EdgeCount(g))
	}

	removed := RemoveNode(g, a)
	if !removed {
		t.Error("expected RemoveNode to return true")
	}
	if NodeCount(g) != 1 {
		t.Errorf("expected 1 node after remove, got %d", NodeCount(g))
	}
	if EdgeCount(g) != 0 {
		t.Errorf("expected 0 edges after remove (edge should be detached), got %d", EdgeCount(g))
	}
}

func TestRemoveNode_Nil(t *testing.T) {
	g := lpg.NewGraph()
	removed := RemoveNode(g, nil)
	if removed {
		t.Error("expected RemoveNode(nil) to return false")
	}
}

func TestRemoveNodeByID(t *testing.T) {
	g := lpg.NewGraph()
	AddNode(g, LabelFunction, map[string]any{PropID: "a", PropName: "A"})
	AddNode(g, LabelFunction, map[string]any{PropID: "b", PropName: "B"})

	removed := RemoveNodeByID(g, "a")
	if !removed {
		t.Error("expected RemoveNodeByID to return true")
	}
	if NodeCount(g) != 1 {
		t.Errorf("expected 1 node, got %d", NodeCount(g))
	}
	if FindNodeByID(g, "a") != nil {
		t.Error("expected node 'a' to be removed")
	}
}

func TestRemoveNodeByID_NotFound(t *testing.T) {
	g := lpg.NewGraph()
	removed := RemoveNodeByID(g, "nonexistent")
	if removed {
		t.Error("expected RemoveNodeByID to return false for missing node")
	}
}

func TestRemoveNodesByFilePath(t *testing.T) {
	g := lpg.NewGraph()
	AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "main.go",
		StartLine:     12,
		EndLine:       20,
	})
	AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:other", Name: "other"},
		FilePath:      "other.go",
		StartLine:     1,
		EndLine:       10,
	})

	count := RemoveNodesByFilePath(g, "main.go")
	if count != 2 {
		t.Errorf("expected 2 nodes removed, got %d", count)
	}
	if NodeCount(g) != 1 {
		t.Errorf("expected 1 remaining node, got %d", NodeCount(g))
	}
	if FindNodeByID(g, "func:other") == nil {
		t.Error("expected func:other to remain")
	}
}

func TestRemoveNodesByFilePath_EdgesCleanedUp(t *testing.T) {
	g := lpg.NewGraph()
	a := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:a", Name: "a"},
		FilePath:      "target.go",
		StartLine:     1,
		EndLine:       10,
	})
	b := AddSymbolNode(g, LabelFunction, SymbolProps{
		BaseNodeProps: BaseNodeProps{ID: "func:b", Name: "b"},
		FilePath:      "other.go",
		StartLine:     1,
		EndLine:       10,
	})
	AddEdge(g, b, a, RelCalls, nil)

	RemoveNodesByFilePath(g, "target.go")
	if EdgeCount(g) != 0 {
		t.Errorf("expected edges to be cleaned up, got %d", EdgeCount(g))
	}
}
