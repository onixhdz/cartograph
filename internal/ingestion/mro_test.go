package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

const testMethodFoo = "foo"

func TestC3Linearize_SingleInheritance(t *testing.T) {
	hierarchy := map[string][]string{
		"C": {"B"},
		"B": {"A"},
		"A": {},
	}

	mro, err := C3Linearize(hierarchy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"C", "B", "A"}
	got := mro["C"]
	if !slicesEqual(got, expected) {
		t.Errorf("MRO(C) = %v, want %v", got, expected)
	}

	expectedB := []string{"B", "A"}
	gotB := mro["B"]
	if !slicesEqual(gotB, expectedB) {
		t.Errorf("MRO(B) = %v, want %v", gotB, expectedB)
	}
}

func TestC3Linearize_Diamond(t *testing.T) {
	// D -> (B, C), B -> A, C -> A
	hierarchy := map[string][]string{
		"D": {"B", "C"},
		"B": {"A"},
		"C": {"A"},
		"A": {},
	}

	mro, err := C3Linearize(hierarchy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"D", "B", "C", "A"}
	got := mro["D"]
	if !slicesEqual(got, expected) {
		t.Errorf("MRO(D) = %v, want %v", got, expected)
	}
}

func TestC3Linearize_NoParents(t *testing.T) {
	hierarchy := map[string][]string{
		"X": {},
	}

	mro, err := C3Linearize(hierarchy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"X"}
	got := mro["X"]
	if !slicesEqual(got, expected) {
		t.Errorf("MRO(X) = %v, want %v", got, expected)
	}
}

func TestC3Linearize_CycleReturnsError(t *testing.T) {
	hierarchy := map[string][]string{
		"A": {"B"},
		"B": {"A"},
	}

	_, err := C3Linearize(hierarchy)
	if err == nil {
		t.Error("expected error for cyclic hierarchy, got nil")
	}
}

func TestComputeOverrides_MatchingMethod(t *testing.T) {
	g := lpg.NewGraph()

	// Parent class A with method "run".
	classA := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	methodArun := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:A.run", Name: "run"},
		FilePath:      "a.go",
		StartLine:     3,
		EndLine:       8,
	})
	graph.AddEdge(g, classA, methodArun, graph.RelHasMethod, nil)

	// Child class B extends A with method "run".
	classB := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	methodBrun := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:B.run", Name: "run"},
		FilePath:      "b.go",
		StartLine:     3,
		EndLine:       8,
	})
	graph.AddEdge(g, classB, methodBrun, graph.RelHasMethod, nil)

	mro := map[string][]string{
		"B": {"B", "A"},
		"A": {"A"},
	}

	count := ComputeOverrides(g, mro)
	if count != 1 {
		t.Errorf("expected 1 override, got %d", count)
	}

	// Check OVERRIDES edge exists from B.run → A.run.
	edges := graph.GetOutgoingEdges(methodBrun, graph.RelOverrides)
	if len(edges) != 1 {
		t.Fatalf("expected 1 OVERRIDES edge, got %d", len(edges))
	}
	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "method:A.run" {
		t.Errorf("expected override target method:A.run, got %s", targetID)
	}
}

func TestComputeOverrides_NoMatchingMethod(t *testing.T) {
	g := lpg.NewGraph()

	classA := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:A.foo", Name: testMethodFoo},
		FilePath:      "a.go",
		StartLine:     3,
		EndLine:       8,
	})
	graph.AddEdge(g, classA, graph.FindNodeByID(g, "method:A.foo"), graph.RelHasMethod, nil)

	classB := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:B.bar", Name: "bar"},
		FilePath:      "b.go",
		StartLine:     3,
		EndLine:       8,
	})
	graph.AddEdge(g, classB, graph.FindNodeByID(g, "method:B.bar"), graph.RelHasMethod, nil)

	mro := map[string][]string{
		"B": {"B", "A"},
		"A": {"A"},
	}

	count := ComputeOverrides(g, mro)
	if count != 0 {
		t.Errorf("expected 0 overrides (different method names), got %d", count)
	}
}

func TestComputeOverrides_ReturnsCorrectCount(t *testing.T) {
	g := lpg.NewGraph()

	// A has methods: run, stop
	classA := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       20,
	})
	mArun := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:A.run", Name: "run"},
		FilePath:      "a.go",
		StartLine:     3,
		EndLine:       8,
	})
	mAstop := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:A.stop", Name: "stop"},
		FilePath:      "a.go",
		StartLine:     10,
		EndLine:       15,
	})
	graph.AddEdge(g, classA, mArun, graph.RelHasMethod, nil)
	graph.AddEdge(g, classA, mAstop, graph.RelHasMethod, nil)

	// B extends A, has methods: run, stop
	classB := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       20,
	})
	mBrun := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:B.run", Name: "run"},
		FilePath:      "b.go",
		StartLine:     3,
		EndLine:       8,
	})
	mBstop := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:B.stop", Name: "stop"},
		FilePath:      "b.go",
		StartLine:     10,
		EndLine:       15,
	})
	graph.AddEdge(g, classB, mBrun, graph.RelHasMethod, nil)
	graph.AddEdge(g, classB, mBstop, graph.RelHasMethod, nil)

	mro := map[string][]string{
		"B": {"B", "A"},
		"A": {"A"},
	}

	count := ComputeOverrides(g, mro)
	if count != 2 {
		t.Errorf("expected 2 overrides, got %d", count)
	}

	// Verify the edges.
	_ = mArun
	_ = mAstop
	runEdges := graph.GetOutgoingEdges(mBrun, graph.RelOverrides)
	if len(runEdges) != 1 {
		t.Errorf("expected 1 OVERRIDES edge from B.run, got %d", len(runEdges))
	}
	stopEdges := graph.GetOutgoingEdges(mBstop, graph.RelOverrides)
	if len(stopEdges) != 1 {
		t.Errorf("expected 1 OVERRIDES edge from B.stop, got %d", len(stopEdges))
	}
}

// slicesEqual compares two string slices for equality.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] { //nolint:gosec // G602 — bounds checked by len comparison above //nolint:gosec // G602 — bounds checked by len comparison above
			return false
		}
	}
	return true
}

// --- Language-Aware MRO Tests (ComputeMRO) ---

func addTestClass(g *lpg.Graph, name, language string, label graph.NodeLabel) *lpg.Node {
	return graph.AddNode(g, label, map[string]any{
		graph.PropID:       string(label) + ":" + name,
		graph.PropName:     name,
		graph.PropFilePath: "src/" + name + ".ts",
		graph.PropLanguage: language,
	})
}

func addTestMethod(g *lpg.Graph, className, methodName string, classNode *lpg.Node) *lpg.Node {
	methodID := "method:" + className + "." + methodName
	m := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: methodID, Name: methodName},
		FilePath:      "src/" + className + ".ts",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddEdge(g, classNode, m, graph.RelHasMethod, nil)
	return m
}

func addTestProperty(g *lpg.Graph, className, propName string, classNode *lpg.Node) {
	propID := "property:" + className + "." + propName
	p := graph.AddNode(g, graph.LabelProperty, map[string]any{
		graph.PropID:       propID,
		graph.PropName:     propName,
		graph.PropFilePath: "src/" + className + ".ts",
	})
	graph.AddEdge(g, classNode, p, graph.RelHasMethod, nil)
}

func TestComputeMRO_CppDiamond_LeftmostWins(t *testing.T) {
	g := lpg.NewGraph()

	a := addTestClass(g, "A", "cpp", graph.LabelClass)
	b := addTestClass(g, "B", "cpp", graph.LabelClass)
	c := addTestClass(g, "C", "cpp", graph.LabelClass)
	d := addTestClass(g, "D", "cpp", graph.LabelClass)

	graph.AddEdge(g, b, a, graph.RelExtends, nil)
	graph.AddEdge(g, c, a, graph.RelExtends, nil)
	graph.AddEdge(g, d, b, graph.RelExtends, nil) // B is leftmost
	graph.AddEdge(g, d, c, graph.RelExtends, nil)

	addTestMethod(g, "A", testMethodFoo, a)
	bFoo := addTestMethod(g, "B", testMethodFoo, b)
	addTestMethod(g, "C", testMethodFoo, c)

	result := ComputeMRO(g)

	var dEntry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "D" {
			dEntry = &result.Entries[i]
			break
		}
	}
	if dEntry == nil {
		t.Fatal("expected entry for D")
		return
	}
	if dEntry.Language != "cpp" {
		t.Errorf("expected language cpp, got %s", dEntry.Language)
	}

	var fooAmb *MROAmbiguity
	for i, a := range dEntry.Ambiguities {
		if a.MethodName == testMethodFoo {
			fooAmb = &dEntry.Ambiguities[i]
			break
		}
	}
	if fooAmb == nil {
		t.Fatal("expected foo ambiguity for D")
		return
	}
	if len(fooAmb.DefinedIn) < 2 {
		t.Errorf("expected at least 2 definitions, got %d", len(fooAmb.DefinedIn))
	}

	bFooID := graph.GetStringProp(bFoo, graph.PropID)
	if fooAmb.ResolvedTo != bFooID {
		t.Errorf("expected leftmost (B) to win, resolved to %q, want %q", fooAmb.ResolvedTo, bFooID)
	}
	if fooAmb.Reason == "" || !contains(fooAmb.Reason, "C++ leftmost") {
		t.Errorf("expected reason to contain 'C++ leftmost', got %q", fooAmb.Reason)
	}
}

func TestComputeMRO_CppDiamond_NoAmbiguityWhenOnlyBaseHasMethod(t *testing.T) {
	g := lpg.NewGraph()

	a := addTestClass(g, "A", "cpp", graph.LabelClass)
	b := addTestClass(g, "B", "cpp", graph.LabelClass)
	c := addTestClass(g, "C", "cpp", graph.LabelClass)
	addTestClass(g, "D", "cpp", graph.LabelClass)

	graph.AddEdge(g, b, a, graph.RelExtends, nil)
	graph.AddEdge(g, c, a, graph.RelExtends, nil)

	// D extends B and C, but only A has foo (seen through both paths via B and C)
	dNode := graph.FindNodeByID(g, "Class:D")
	graph.AddEdge(g, dNode, b, graph.RelExtends, nil)
	graph.AddEdge(g, dNode, c, graph.RelExtends, nil)

	// Only A has foo — no collision since it's the same method node
	addTestMethod(g, "A", testMethodFoo, a)

	result := ComputeMRO(g)

	var dEntry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "D" {
			dEntry = &result.Entries[i]
			break
		}
	}
	if dEntry == nil {
		t.Fatal("expected entry for D")
		return
	}

	// B and C don't define foo, so D inherits A::foo through both — but it's only
	// defined in B and C's HAS_METHOD if they have their own. Since B and C don't
	// have foo, there should be no ambiguity at D's direct parent level.
	for _, amb := range dEntry.Ambiguities {
		if amb.MethodName == testMethodFoo {
			t.Error("expected no foo ambiguity when only A defines it")
		}
	}
}

func TestComputeMRO_CSharp_ClassBeatsInterface(t *testing.T) {
	g := lpg.NewGraph()

	baseClass := addTestClass(g, "BaseClass", "csharp", graph.LabelClass)
	iface := addTestClass(g, "IDoSomething", "csharp", graph.LabelInterface)
	child := addTestClass(g, "MyClass", "csharp", graph.LabelClass)

	graph.AddEdge(g, child, baseClass, graph.RelExtends, nil)
	graph.AddEdge(g, child, iface, graph.RelImplements, nil)

	baseDoIt := addTestMethod(g, "BaseClass", "doIt", baseClass)
	addTestMethod(g, "IDoSomething", "doIt", iface)

	result := ComputeMRO(g)

	var entry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "MyClass" {
			entry = &result.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected entry for MyClass")
	}

	var doItAmb *MROAmbiguity
	for i, a := range entry.Ambiguities {
		if a.MethodName == "doIt" {
			doItAmb = &entry.Ambiguities[i]
			break
		}
	}
	if doItAmb == nil {
		t.Fatal("expected doIt ambiguity")
	}

	baseDoItID := graph.GetStringProp(baseDoIt, graph.PropID)
	if doItAmb.ResolvedTo != baseDoItID {
		t.Errorf("expected class method to win, resolved to %q, want %q", doItAmb.ResolvedTo, baseDoItID)
	}
	if !contains(doItAmb.Reason, "class method wins") {
		t.Errorf("expected reason to contain 'class method wins', got %q", doItAmb.Reason)
	}
}

func TestComputeMRO_CSharp_MultipleInterfacesAmbiguous(t *testing.T) {
	g := lpg.NewGraph()

	child := addTestClass(g, "MyClass", "csharp", graph.LabelClass)
	iFoo := addTestClass(g, "IFoo", "csharp", graph.LabelInterface)
	iBar := addTestClass(g, "IBar", "csharp", graph.LabelInterface)

	graph.AddEdge(g, child, iFoo, graph.RelImplements, nil)
	graph.AddEdge(g, child, iBar, graph.RelImplements, nil)

	addTestMethod(g, "IFoo", "process", iFoo)
	addTestMethod(g, "IBar", "process", iBar)

	result := ComputeMRO(g)

	var entry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "MyClass" {
			entry = &result.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected entry for MyClass")
	}

	var processAmb *MROAmbiguity
	for i, a := range entry.Ambiguities {
		if a.MethodName == "process" {
			processAmb = &entry.Ambiguities[i]
			break
		}
	}
	if processAmb == nil {
		t.Fatal("expected process ambiguity")
	}
	if processAmb.ResolvedTo != "" {
		t.Errorf("expected null resolution, got %q", processAmb.ResolvedTo)
	}
	if !contains(processAmb.Reason, "ambiguous") {
		t.Errorf("expected reason to contain 'ambiguous', got %q", processAmb.Reason)
	}
	if result.AmbiguityCount < 1 {
		t.Error("expected ambiguityCount >= 1")
	}
}

func TestComputeMRO_Python_C3LeftmostWins(t *testing.T) {
	g := lpg.NewGraph()

	a := addTestClass(g, "A", "python", graph.LabelClass)
	b := addTestClass(g, "B", "python", graph.LabelClass)
	c := addTestClass(g, "C", "python", graph.LabelClass)
	d := addTestClass(g, "D", "python", graph.LabelClass)

	graph.AddEdge(g, b, a, graph.RelExtends, nil)
	graph.AddEdge(g, c, a, graph.RelExtends, nil)
	graph.AddEdge(g, d, b, graph.RelExtends, nil) // B first
	graph.AddEdge(g, d, c, graph.RelExtends, nil)

	addTestMethod(g, "A", testMethodFoo, a)
	bFoo := addTestMethod(g, "B", testMethodFoo, b)
	addTestMethod(g, "C", testMethodFoo, c)

	result := ComputeMRO(g)

	var dEntry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "D" {
			dEntry = &result.Entries[i]
			break
		}
	}
	if dEntry == nil {
		t.Fatal("expected entry for D")
	}

	var fooAmb *MROAmbiguity
	for i, a := range dEntry.Ambiguities {
		if a.MethodName == testMethodFoo {
			fooAmb = &dEntry.Ambiguities[i]
			break
		}
	}
	if fooAmb == nil {
		t.Fatal("expected foo ambiguity")
	}

	bFooID := graph.GetStringProp(bFoo, graph.PropID)
	if fooAmb.ResolvedTo != bFooID {
		t.Errorf("expected B (C3 leftmost) to win, resolved to %q, want %q", fooAmb.ResolvedTo, bFooID)
	}
	if !contains(fooAmb.Reason, "Python C3") {
		t.Errorf("expected reason to contain 'Python C3', got %q", fooAmb.Reason)
	}
}

func TestComputeMRO_Java_ClassBeatsInterface(t *testing.T) {
	g := lpg.NewGraph()

	base := addTestClass(g, "BaseService", "java", graph.LabelClass)
	iface := addTestClass(g, "Runnable", "java", graph.LabelInterface)
	child := addTestClass(g, "Service", "java", graph.LabelClass)

	graph.AddEdge(g, child, base, graph.RelExtends, nil)
	graph.AddEdge(g, child, iface, graph.RelImplements, nil)

	baseRun := addTestMethod(g, "BaseService", "run", base)
	addTestMethod(g, "Runnable", "run", iface)

	result := ComputeMRO(g)

	var entry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "Service" {
			entry = &result.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected entry for Service")
	}

	var runAmb *MROAmbiguity
	for i, a := range entry.Ambiguities {
		if a.MethodName == "run" {
			runAmb = &entry.Ambiguities[i]
			break
		}
	}
	if runAmb == nil {
		t.Fatal("expected run ambiguity")
	}

	baseRunID := graph.GetStringProp(baseRun, graph.PropID)
	if runAmb.ResolvedTo != baseRunID {
		t.Errorf("expected class method to win, resolved to %q, want %q", runAmb.ResolvedTo, baseRunID)
	}
}

func TestComputeMRO_Rust_TraitConflictsNull(t *testing.T) {
	g := lpg.NewGraph()

	myStruct := addTestClass(g, "MyStruct", "rust", graph.LabelStruct)
	traitA := addTestClass(g, "TraitA", "rust", graph.LabelTrait)
	traitB := addTestClass(g, "TraitB", "rust", graph.LabelTrait)

	graph.AddEdge(g, myStruct, traitA, graph.RelImplements, nil)
	graph.AddEdge(g, myStruct, traitB, graph.RelImplements, nil)

	addTestMethod(g, "TraitA", "execute", traitA)
	addTestMethod(g, "TraitB", "execute", traitB)

	result := ComputeMRO(g)

	var entry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "MyStruct" {
			entry = &result.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected entry for MyStruct")
	}

	var execAmb *MROAmbiguity
	for i, a := range entry.Ambiguities {
		if a.MethodName == "execute" {
			execAmb = &entry.Ambiguities[i]
			break
		}
	}
	if execAmb == nil {
		t.Fatal("expected execute ambiguity")
	}
	if execAmb.ResolvedTo != "" {
		t.Errorf("expected null resolution for Rust trait conflict, got %q", execAmb.ResolvedTo)
	}
	if !contains(execAmb.Reason, "qualified syntax") {
		t.Errorf("expected reason to contain 'qualified syntax', got %q", execAmb.Reason)
	}
	if result.AmbiguityCount < 1 {
		t.Error("expected ambiguityCount >= 1")
	}

	// No OVERRIDES edge should be emitted for Rust ambiguity
	overrideCount := 0
	graph.ForEachEdge(g, func(e *lpg.Edge) bool {
		if rt, err := graph.GetEdgeRelType(e); err == nil && rt == graph.RelOverrides {
			fromID := graph.GetStringProp(e.GetFrom(), graph.PropID)
			if fromID == "Struct:MyStruct" {
				overrideCount++
			}
		}
		return true
	})
	if overrideCount != 0 {
		t.Errorf("expected 0 OVERRIDES edges from MyStruct, got %d", overrideCount)
	}
}

func TestComputeMRO_PropertyExcludedFromOverrides(t *testing.T) {
	g := lpg.NewGraph()

	parentA := addTestClass(g, "ParentA", "typescript", graph.LabelClass)
	parentB := addTestClass(g, "ParentB", "typescript", graph.LabelClass)
	child := addTestClass(g, "Child", "typescript", graph.LabelClass)

	graph.AddEdge(g, child, parentA, graph.RelExtends, nil)
	graph.AddEdge(g, child, parentB, graph.RelExtends, nil)

	// Add Property nodes with same name to both parents
	addTestProperty(g, "ParentA", "name", parentA)
	addTestProperty(g, "ParentB", "name", parentB)

	result := ComputeMRO(g)

	// No OVERRIDES edges should be emitted for properties
	if result.OverrideEdges != 0 {
		t.Errorf("expected 0 override edges for properties, got %d", result.OverrideEdges)
	}
}

func TestComputeMRO_PropertyAndMethodMixed(t *testing.T) {
	g := lpg.NewGraph()

	parentA := addTestClass(g, "PA", "cpp", graph.LabelClass)
	parentB := addTestClass(g, "PB", "cpp", graph.LabelClass)
	child := addTestClass(g, "Ch", "cpp", graph.LabelClass)

	graph.AddEdge(g, child, parentA, graph.RelExtends, nil)
	graph.AddEdge(g, child, parentB, graph.RelExtends, nil)

	// Method collision (should trigger OVERRIDES)
	addTestMethod(g, "PA", "doWork", parentA)
	addTestMethod(g, "PB", "doWork", parentB)

	// Property collision (should NOT trigger OVERRIDES)
	addTestProperty(g, "PA", "id", parentA)
	addTestProperty(g, "PB", "id", parentB)

	result := ComputeMRO(g)

	// Only 1 OVERRIDES edge (for the method, not the property)
	if result.OverrideEdges != 1 {
		t.Errorf("expected 1 override edge (method only), got %d", result.OverrideEdges)
	}
}

func TestComputeMRO_SingleParentNoAmbiguity(t *testing.T) {
	g := lpg.NewGraph()

	parent := addTestClass(g, "Parent", "typescript", graph.LabelClass)
	addTestClass(g, "Child", "typescript", graph.LabelClass)

	childNode := graph.FindNodeByID(g, "Class:Child")
	graph.AddEdge(g, childNode, parent, graph.RelExtends, nil)

	addTestMethod(g, "Parent", testMethodFoo, parent)
	addTestMethod(g, "Parent", "bar", parent)

	result := ComputeMRO(g)

	var entry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "Child" {
			entry = &result.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected entry for Child")
	}
	if len(entry.Ambiguities) != 0 {
		t.Errorf("expected 0 ambiguities for single parent, got %d", len(entry.Ambiguities))
	}
}

func TestComputeMRO_StandaloneClassNotInEntries(t *testing.T) {
	g := lpg.NewGraph()

	standalone := addTestClass(g, "Standalone", "typescript", graph.LabelClass)
	addTestMethod(g, "Standalone", "doStuff", standalone)

	result := ComputeMRO(g)

	for _, e := range result.Entries {
		if e.ClassName == "Standalone" {
			t.Error("standalone class should not be in entries")
		}
	}
	if result.OverrideEdges != 0 {
		t.Errorf("expected 0 override edges, got %d", result.OverrideEdges)
	}
	if result.AmbiguityCount != 0 {
		t.Errorf("expected 0 ambiguities, got %d", result.AmbiguityCount)
	}
}

func TestComputeMRO_OwnMethodShadowsAncestor(t *testing.T) {
	g := lpg.NewGraph()

	base1 := addTestClass(g, "Base1", "cpp", graph.LabelClass)
	base2 := addTestClass(g, "Base2", "cpp", graph.LabelClass)
	child := addTestClass(g, "Child", "cpp", graph.LabelClass)

	graph.AddEdge(g, child, base1, graph.RelExtends, nil)
	graph.AddEdge(g, child, base2, graph.RelExtends, nil)

	addTestMethod(g, "Base1", testMethodFoo, base1)
	addTestMethod(g, "Base2", testMethodFoo, base2)
	addTestMethod(g, "Child", testMethodFoo, child) // own method shadows

	result := ComputeMRO(g)

	var entry *MROEntry
	for i, e := range result.Entries {
		if e.ClassName == "Child" {
			entry = &result.Entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("expected entry for Child")
	}
	// No ambiguity because Child defines its own foo
	for _, amb := range entry.Ambiguities {
		if amb.MethodName == testMethodFoo {
			t.Error("expected no foo ambiguity when child defines its own")
		}
	}
}

func TestComputeMRO_EmptyGraph(t *testing.T) {
	g := lpg.NewGraph()
	result := ComputeMRO(g)
	if len(result.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(result.Entries))
	}
	if result.OverrideEdges != 0 {
		t.Errorf("expected 0 override edges, got %d", result.OverrideEdges)
	}
	if result.AmbiguityCount != 0 {
		t.Errorf("expected 0 ambiguities, got %d", result.AmbiguityCount)
	}
}

func TestComputeMRO_CyclicInheritance(t *testing.T) {
	g := lpg.NewGraph()

	a := addTestClass(g, "A", "python", graph.LabelClass)
	b := addTestClass(g, "B", "python", graph.LabelClass)

	graph.AddEdge(g, a, b, graph.RelExtends, nil)
	graph.AddEdge(g, b, a, graph.RelExtends, nil)

	addTestMethod(g, "A", testMethodFoo, a)
	addTestMethod(g, "B", testMethodFoo, b)

	// Should not panic — gracefully handle cycles
	result := ComputeMRO(g)
	// Both have parents, so both should get entries
	if len(result.Entries) < 1 {
		t.Errorf("expected at least 1 entry for cyclic hierarchy, got %d", len(result.Entries))
	}
}

func TestComputeMRO_ThreeNodeCycle(t *testing.T) {
	g := lpg.NewGraph()

	x := addTestClass(g, "X", "python", graph.LabelClass)
	y := addTestClass(g, "Y", "python", graph.LabelClass)
	z := addTestClass(g, "Z", "python", graph.LabelClass)

	graph.AddEdge(g, x, y, graph.RelExtends, nil)
	graph.AddEdge(g, y, z, graph.RelExtends, nil)
	graph.AddEdge(g, z, x, graph.RelExtends, nil)

	// Should not panic
	result := ComputeMRO(g)
	_ = result
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
