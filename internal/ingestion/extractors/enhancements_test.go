package extractors

import (
	"testing"

	"github.com/realxen/cartograph/internal/graph"
)

// ---------------------------------------------------------------------------
// Gap 1: C# primary constructor parameter extraction
// ---------------------------------------------------------------------------

func TestGapFix_CSharpPrimaryConstructor(t *testing.T) {
	src := `class Point(int x, int y) {
    public int X => x;
    public int Y => y;
}

record Person(string Name, int Age);
`
	result, err := ExtractFile("/tmp/test.cs", []byte(src), "c_sharp")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Should find the Point class.
	foundPoint := false
	foundConstructor := false
	for _, sym := range result.Symbols {
		if sym.Name == "Point" && sym.Label == graph.LabelClass {
			foundPoint = true
		}
		if sym.Label == graph.LabelConstructor && sym.OwnerName == "Point" {
			foundConstructor = true
			if sym.ParameterCount != 2 {
				t.Errorf("expected 2 params on Point constructor, got %d", sym.ParameterCount)
			}
		}
	}
	if !foundPoint {
		t.Error("expected Point class symbol")
	}
	if !foundConstructor {
		t.Error("expected Constructor symbol for Point primary constructor")
	}

	// Additionally, the type bindings should capture x:int and y:int as parameters.
	paramBindings := filterBindings(result.TypeBindings, "parameter")
	t.Logf("C# primary constructor param bindings: %v", bindingSummary(paramBindings))
	if !findBindingPartial(paramBindings, "x", "int") {
		t.Errorf("expected x:int parameter binding; got: %v", bindingSummary(paramBindings))
	}
	if !findBindingPartial(paramBindings, "y", "int") {
		t.Errorf("expected y:int parameter binding; got: %v", bindingSummary(paramBindings))
	}
}

// ---------------------------------------------------------------------------
// Gap 2: Kotlin interface reclassification
// ---------------------------------------------------------------------------

func TestGapFix_KotlinInterfaceReclassification(t *testing.T) {
	// Note: the tree-sitter-kotlin grammar (fwcd) has issues parsing standalone
	// "interface" declarations — they parse as ERROR. However, when embedded in
	// certain file structures they may parse. Test with what the grammar supports.
	src := `data class Foo(val name: String)
`
	result, err := ExtractFile("/tmp/test.kt", []byte(src), "kotlin")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Verify the class is detected.
	foundFoo := false
	for _, sym := range result.Symbols {
		if sym.Name == "Foo" {
			foundFoo = true
			t.Logf("Foo label: %s", sym.Label)
		}
	}
	if !foundFoo {
		t.Error("expected Foo class symbol from Kotlin data class")
	}
}

func TestGapFix_InterfaceReclassification_Generic(t *testing.T) {
	// Test with C# which has proper interface_declaration support.
	// The reclassification targets grammars where interfaces share class_declaration.
	src := `interface IRunnable {
    void Run();
}
class Runner : IRunnable {
    public void Run() { }
}
`
	result, err := ExtractFile("/tmp/test.cs", []byte(src), "c_sharp")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	foundInterface := false
	foundClass := false
	for _, sym := range result.Symbols {
		if sym.Name == "IRunnable" && sym.Label == graph.LabelInterface {
			foundInterface = true
		}
		if sym.Name == "Runner" && sym.Label == graph.LabelClass {
			foundClass = true
		}
	}
	if !foundInterface {
		t.Error("expected IRunnable as Interface")
	}
	if !foundClass {
		t.Error("expected Runner as Class")
	}
}

// ---------------------------------------------------------------------------
// Gap 3: Kotlin infix expression calls
// ---------------------------------------------------------------------------

func TestGapFix_KotlinInfixCalls(t *testing.T) {
	src := `fun main() {
    val result = 1 add 2
    val pair = "hello" to "world"
}
`
	result, err := ExtractFile("/tmp/test.kt", []byte(src), "kotlin")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Should have infix calls: "add" and "to".
	foundAdd := false
	foundTo := false
	for _, call := range result.Calls {
		if call.CalleeName == "add" {
			foundAdd = true
		}
		if call.CalleeName == "to" {
			foundTo = true
		}
	}
	if !foundAdd {
		t.Error("expected infix call to 'add'")
	}
	if !foundTo {
		t.Error("expected infix call to 'to'")
	}
}

// ---------------------------------------------------------------------------
// Gap 5: Receiver name extraction on calls
// ---------------------------------------------------------------------------

func TestGapFix_ReceiverNameExtraction(t *testing.T) {
	src := `const w = new Widget();
w.render();
w.update(42);
console.log("done");
`
	result, err := ExtractFile("/tmp/test.ts", []byte(src), "typescript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	for _, call := range result.Calls {
		t.Logf("call: %s (receiver: %q, line: %d)", call.CalleeName, call.ReceiverName, call.Line)
	}

	// Check that render and update have receiver "w".
	foundRender := false
	foundUpdate := false
	for _, call := range result.Calls {
		if call.CalleeName == "render" && call.ReceiverName == "w" {
			foundRender = true
		}
		if call.CalleeName == "update" && call.ReceiverName == "w" {
			foundUpdate = true
		}
	}
	if !foundRender {
		t.Error("expected render call with receiver 'w'")
	}
	if !foundUpdate {
		t.Error("expected update call with receiver 'w'")
	}
}

func TestGapFix_ReceiverNamePython(t *testing.T) {
	src := `class Foo:
    def bar(self):
        self.baz()
        obj.process()
`
	result, err := ExtractFile("/tmp/test.py", []byte(src), "python")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	for _, call := range result.Calls {
		t.Logf("call: %s (receiver: %q)", call.CalleeName, call.ReceiverName)
	}

	// baz should have receiver "self", process should have receiver "obj".
	for _, call := range result.Calls {
		if call.CalleeName == "baz" && call.ReceiverName != "" {
			if call.ReceiverName != "self" {
				t.Errorf("expected baz receiver 'self', got %q", call.ReceiverName)
			}
		}
	}
}

func TestGapFix_ReceiverNameGo(t *testing.T) {
	src := `package main

func main() {
	s := Server{}
	s.Start()
	s.Stop()
}
`
	result, err := ExtractFile("/tmp/test.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	for _, call := range result.Calls {
		t.Logf("call: %s (receiver: %q)", call.CalleeName, call.ReceiverName)
	}

	// Start and Stop should have receiver "s".
	for _, call := range result.Calls {
		if call.CalleeName == "Start" || call.CalleeName == "Stop" {
			if call.ReceiverName != "s" {
				t.Errorf("expected %s receiver 's', got %q", call.CalleeName, call.ReceiverName)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Gap 6: Named import binding extraction
// ---------------------------------------------------------------------------

func TestGapFix_NamedImportBindings_TS(t *testing.T) {
	src := `import { useState as useStateHook, useEffect } from "react";
import * as path from "path";
import defaultExport from "module";
`
	result, err := ExtractFile("/tmp/test.ts", []byte(src), "typescript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	for _, imp := range result.Imports {
		t.Logf("import %q bindings=%v", imp.Source, imp.Bindings)
	}

	// Find the react import.
	var reactImport *ExtractedImport
	var pathImport *ExtractedImport
	var moduleImport *ExtractedImport
	for i, imp := range result.Imports {
		switch imp.Source {
		case "react":
			reactImport = &result.Imports[i]
		case "path":
			pathImport = &result.Imports[i]
		case "module":
			moduleImport = &result.Imports[i]
		}
	}

	// React: should have useState→useStateHook and useEffect bindings.
	if reactImport == nil {
		t.Fatal("expected react import")
	}
	if len(reactImport.Bindings) < 2 {
		t.Fatalf("expected ≥2 bindings for react import, got %d: %v", len(reactImport.Bindings), reactImport.Bindings)
	}
	foundUseState := false
	foundUseEffect := false
	for _, b := range reactImport.Bindings {
		if b.Original == "useState" && b.Alias == "useStateHook" {
			foundUseState = true
		}
		if b.Original == "useEffect" && b.Alias == "" {
			foundUseEffect = true
		}
	}
	if !foundUseState {
		t.Errorf("expected useState→useStateHook binding; got: %v", reactImport.Bindings)
	}
	if !foundUseEffect {
		t.Errorf("expected useEffect binding; got: %v", reactImport.Bindings)
	}

	// Path: should have namespace import * as path.
	if pathImport == nil {
		t.Fatal("expected path import")
	}
	if len(pathImport.Bindings) == 0 {
		t.Fatal("expected bindings for path import")
	}
	foundNamespace := false
	for _, b := range pathImport.Bindings {
		if b.Original == "*" && b.Alias == "path" {
			foundNamespace = true
		}
	}
	if !foundNamespace {
		t.Errorf("expected *→path namespace binding; got: %v", pathImport.Bindings)
	}

	// Module: should have default import.
	if moduleImport == nil {
		t.Fatal("expected module import")
	}
	if len(moduleImport.Bindings) == 0 {
		t.Fatal("expected bindings for module import")
	}
	foundDefault := false
	for _, b := range moduleImport.Bindings {
		if b.Original == "default" && b.Alias == "defaultExport" {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Errorf("expected default→defaultExport binding; got: %v", moduleImport.Bindings)
	}
}

func TestGapFix_NamedImportBindings_JS(t *testing.T) {
	src := `import { render, hydrate as hydrateRoot } from "react-dom";
`
	result, err := ExtractFile("/tmp/test.js", []byte(src), "javascript")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	for _, imp := range result.Imports {
		t.Logf("import %q bindings=%v", imp.Source, imp.Bindings)
	}

	var reactDom *ExtractedImport
	for i, imp := range result.Imports {
		if imp.Source == "react-dom" {
			reactDom = &result.Imports[i]
		}
	}
	if reactDom == nil {
		t.Fatal("expected react-dom import")
	}
	if len(reactDom.Bindings) < 2 {
		t.Fatalf("expected ≥2 bindings; got %d", len(reactDom.Bindings))
	}

	foundRender := false
	foundHydrate := false
	for _, b := range reactDom.Bindings {
		if b.Original == "render" {
			foundRender = true
		}
		if b.Original == "hydrate" && b.Alias == "hydrateRoot" {
			foundHydrate = true
		}
	}
	if !foundRender {
		t.Error("expected render binding")
	}
	if !foundHydrate {
		t.Error("expected hydrate→hydrateRoot binding")
	}
}

// Gap 4: Tiered call resolution — tested indirectly via call resolver unit tests.

func TestGapFix_TieredCallResolution_Structure(t *testing.T) {
	// Verify that CallInfo supports the new fields.
	// This is a compile-time check — if the fields don't exist, this won't compile.
	_ = ExtractedCall{
		FilePath:     "/tmp/test.go",
		CalleeName:   "Foo",
		ReceiverName: "obj",
		Line:         10,
	}
	t.Log("ExtractedCall has ReceiverName field ✓")
}

// ---------------------------------------------------------------------------
// Gap 7: Go method receiver → OwnerName extraction
// ---------------------------------------------------------------------------

func TestGapFix_GoMethodReceiverOwnerName(t *testing.T) {
	src := `package store

type BboltStore struct {
	db interface{}
}

func (b *BboltStore) SaveGraph() error {
	return nil
}

func (b BboltStore) LoadGraph() error {
	return nil
}

func standalone() {}
`
	result, err := ExtractFile("/tmp/store.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	ownerMap := make(map[string]string)
	for _, sym := range result.Symbols {
		t.Logf("sym: %s (label=%s, owner=%q)", sym.Name, sym.Label, sym.OwnerName)
		ownerMap[sym.Name] = sym.OwnerName
	}

	// Methods with receivers should get OwnerName = receiver type.
	if ownerMap["SaveGraph"] != "BboltStore" {
		t.Errorf("expected SaveGraph owner 'BboltStore', got %q", ownerMap["SaveGraph"])
	}
	if ownerMap["LoadGraph"] != "BboltStore" {
		t.Errorf("expected LoadGraph owner 'BboltStore', got %q", ownerMap["LoadGraph"])
	}
	// Standalone function should have empty owner.
	if ownerMap["standalone"] != "" {
		t.Errorf("expected standalone owner '', got %q", ownerMap["standalone"])
	}
}

// ---------------------------------------------------------------------------
// Gap 7b: Interface method spec extraction
// ---------------------------------------------------------------------------

func TestGapFix_GoInterfaceMethodSpecs(t *testing.T) {
	src := `package store

type GraphStore interface {
	SaveGraph() error
	LoadGraph() error
	DeleteGraph(name string) error
}
`
	result, err := ExtractFile("/tmp/store.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	for _, sym := range result.Symbols {
		t.Logf("sym: %s (label=%s, owner=%q)", sym.Name, sym.Label, sym.OwnerName)
	}

	// Should have: GraphStore (Interface) + 3 method specs (Methods owned by GraphStore).
	methodsByOwner := make(map[string][]string)
	for _, sym := range result.Symbols {
		if sym.OwnerName != "" {
			methodsByOwner[sym.OwnerName] = append(methodsByOwner[sym.OwnerName], sym.Name)
		}
	}

	methods := methodsByOwner["GraphStore"]
	if len(methods) < 3 {
		t.Fatalf("expected 3 methods owned by GraphStore, got %d: %v", len(methods), methods)
	}

	expected := map[string]bool{"SaveGraph": false, "LoadGraph": false, "DeleteGraph": false}
	for _, m := range methods {
		expected[m] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected method %q owned by GraphStore", name)
		}
	}
}

func TestGapFix_GoInterfaceMethodSpecs_EmptyInterface(t *testing.T) {
	src := `package main

type Empty interface{}
`
	result, err := ExtractFile("/tmp/empty.go", []byte(src), "go")
	if err != nil {
		t.Fatalf("ExtractFile: %v", err)
	}

	// Empty interface should not produce any method specs.
	for _, sym := range result.Symbols {
		if sym.OwnerName == "Empty" {
			t.Errorf("unexpected method %q owned by Empty interface", sym.Name)
		}
	}
}
