package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realxen/cartograph/internal/graph"
)

func TestSampleGraph(t *testing.T) {
	g := SampleGraph()

	counts := map[graph.NodeLabel]int{
		graph.LabelFolder:    1,
		graph.LabelFile:      2,
		graph.LabelFunction:  2,
		graph.LabelClass:     1,
		graph.LabelMethod:    1,
		graph.LabelCommunity: 1,
		graph.LabelProcess:   1,
	}
	for label, expected := range counts {
		nodes := graph.FindNodesByLabel(g, label)
		if len(nodes) != expected {
			t.Errorf("expected %d %s nodes, got %d", expected, label, len(nodes))
		}
	}

	// Verify total: 1+2+2+1+1+1+1 = 9 nodes
	if graph.NodeCount(g) != 9 {
		t.Errorf("expected 9 total nodes, got %d", graph.NodeCount(g))
	}

	// Expected edges: 5 CONTAINS + 2 CALLS + 1 HAS_METHOD + 3 MEMBER_OF + 3 STEP_IN_PROCESS = 14
	if graph.EdgeCount(g) != 14 {
		t.Errorf("expected 14 total edges, got %d", graph.EdgeCount(g))
	}
}

func TestSampleGraphNodeLookups(t *testing.T) {
	g := SampleGraph()

	mainFn := MustFindNode(g, "func:main")
	if graph.GetStringProp(mainFn, graph.PropName) != "main" {
		t.Error("main function name mismatch")
	}

	helper := MustFindNode(g, "func:helper")
	if !graph.GetBoolProp(helper, graph.PropIsExported) {
		t.Error("helper should be exported")
	}

	svc := MustFindNode(g, "class:Service")
	if graph.GetIntProp(svc, graph.PropStartLine) != 30 {
		t.Error("Service startLine mismatch")
	}

	process := MustFindNode(g, "process:main-flow")
	if graph.GetStringProp(process, graph.PropEntryPoint) != "func:main" {
		t.Error("process entryPoint mismatch")
	}
}

func TestSampleGraphEdges(t *testing.T) {
	g := SampleGraph()

	mainFn := MustFindNode(g, "func:main")

	callEdges := graph.GetOutgoingEdges(mainFn, graph.RelCalls)
	if len(callEdges) != 2 {
		t.Errorf("expected 2 outgoing CALLS from main, got %d", len(callEdges))
	}

	memberEdges := graph.GetOutgoingEdges(mainFn, graph.RelMemberOf)
	if len(memberEdges) != 1 {
		t.Errorf("expected 1 outgoing MEMBER_OF from main, got %d", len(memberEdges))
	}

	helper := MustFindNode(g, "func:helper")
	inCalls := graph.GetIncomingEdges(helper, graph.RelCalls)
	if len(inCalls) != 1 {
		t.Errorf("expected 1 incoming CALLS to helper, got %d", len(inCalls))
	}
}

func TestMustFindNodePanics(t *testing.T) {
	g := SampleGraph()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for missing node")
		}
	}()
	MustFindNode(g, "nonexistent")
}

func TestTempDir(t *testing.T) {
	dir := TempDir(t, map[string]string{
		"main.go":          "package main",
		"src/utils.go":     "package src",
		"src/nested/deep/": "", // directory only
	})

	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("main.go not found: %v", err)
	}
	if string(data) != "package main" {
		t.Errorf("main.go content mismatch")
	}

	data, err = os.ReadFile(filepath.Join(dir, "src", "utils.go"))
	if err != nil {
		t.Fatalf("src/utils.go not found: %v", err)
	}
	if string(data) != "package src" {
		t.Errorf("src/utils.go content mismatch")
	}

	info, err := os.Stat(filepath.Join(dir, "src", "nested", "deep"))
	if err != nil {
		t.Fatalf("nested dir not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestAssertHelpers(t *testing.T) {
	g := SampleGraph()

	AssertNodeCount(t, g, 9)
	AssertEdgeCount(t, g, 14)
	AssertHasNode(t, g, "func:main")
	AssertHasNode(t, g, "func:helper")
	AssertHasNoNode(t, g, "nonexistent")
	AssertLabelCount(t, g, graph.LabelFunction, 2)
	AssertLabelCount(t, g, graph.LabelFile, 2)
	AssertLabelCount(t, g, graph.LabelProcess, 1)

	mainFn := MustFindNode(g, "func:main")
	AssertHasEdge(t, mainFn, "func:helper", graph.RelCalls)
	AssertHasEdge(t, mainFn, "method:Service.Run", graph.RelCalls)
}
