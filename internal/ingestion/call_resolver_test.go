package ingestion

import (
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
)

func setupCallGraph() *lpg.Graph {
	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "src/main.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "src/utils.go",
		StartLine:     1,
		EndLine:       5,
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Service.Run", Name: "Run"},
		FilePath:      "src/main.go",
		StartLine:     12,
		EndLine:       20,
	})
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Service", Name: "Service"},
		FilePath:      "src/main.go",
		StartLine:     11,
		EndLine:       21,
	})
	return g
}

func TestResolveCalls_DirectFunctionCall(t *testing.T) {
	g := setupCallGraph()

	calls := []CallInfo{
		{CallerNodeID: "func:main", CalleeName: "helper", Confidence: 0.95, Reason: "direct call"},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Errorf("expected 1 resolved call, got %d", count)
	}

	mainNode := graph.FindNodeByID(g, "func:main")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Errorf("expected 1 CALLS edge, got %d", len(edges))
	}
}

func TestResolveCalls_MethodCall(t *testing.T) {
	g := setupCallGraph()

	calls := []CallInfo{
		{CallerNodeID: "func:main", CalleeName: "Run", Confidence: 0.9, Reason: "method call"},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Errorf("expected 1 resolved call, got %d", count)
	}

	mainNode := graph.FindNodeByID(g, "func:main")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	target := edges[0].GetTo()
	targetID := graph.GetStringProp(target, graph.PropID)
	if targetID != "method:Service.Run" {
		t.Errorf("expected target method:Service.Run, got %s", targetID)
	}
}

func TestResolveCalls_AmbiguousCall(t *testing.T) {
	g := setupCallGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper2", Name: "helper"},
		FilePath:      "src/other.go",
		StartLine:     1,
		EndLine:       5,
	})

	calls := []CallInfo{
		{CallerNodeID: "func:main", CalleeName: "helper", Confidence: 0.5, Reason: "ambiguous"},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Errorf("expected 1 resolved call (picks one), got %d", count)
	}
}

func TestResolveCalls_UnresolvableSkipped(t *testing.T) {
	g := setupCallGraph()

	calls := []CallInfo{
		{CallerNodeID: "func:main", CalleeName: "nonExistent", Confidence: 0.8, Reason: "missing"},
	}

	count := ResolveCalls(g, calls)
	if count != 0 {
		t.Errorf("expected 0 resolved calls for nonexistent, got %d", count)
	}
}

func TestResolveCalls_PropertiesSetOnEdge(t *testing.T) {
	g := setupCallGraph()

	calls := []CallInfo{
		{CallerNodeID: "func:main", CalleeName: "helper", Confidence: 0.85, Reason: "static analysis"},
	}

	ResolveCalls(g, calls)

	mainNode := graph.FindNodeByID(g, "func:main")
	edges := graph.GetOutgoingEdges(mainNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	edge := edges[0]
	if v, ok := edge.GetProperty(graph.PropConfidence); !ok {
		t.Error("confidence property not set on edge")
	} else if conf, ok := v.(float64); !ok || conf != 0.85 {
		t.Errorf("confidence: expected 0.85, got %v", v)
	}

	if v, ok := edge.GetProperty(graph.PropReason); !ok {
		t.Error("reason property not set on edge")
	} else if reason, ok := v.(string); !ok || reason != "static analysis" {
		t.Errorf("reason: expected %q, got %v", "static analysis", v)
	}
}

func TestResolveCalls_ReturnsCorrectCount(t *testing.T) {
	g := setupCallGraph()

	calls := []CallInfo{
		{CallerNodeID: "func:main", CalleeName: "helper", Confidence: 0.9, Reason: "direct"},
		{CallerNodeID: "func:main", CalleeName: "Run", Confidence: 0.9, Reason: "method"},
		{CallerNodeID: "func:main", CalleeName: "missing", Confidence: 0.5, Reason: "guess"},
	}

	count := ResolveCalls(g, calls)
	if count != 2 {
		t.Errorf("expected 2 resolved calls (1 missing), got %d", count)
	}
}

func TestResolveCalls_PrefersCallableNodes(t *testing.T) {
	// When a name matches both a Function and a Class, prefer Function.
	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:caller", Name: "caller"},
		FilePath:      "a.go",
	})
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Widget", Name: "Widget"},
		FilePath:      "b.go",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Widget", Name: "Widget"},
		FilePath:      "b.go",
	})

	calls := []CallInfo{
		{CallerNodeID: "func:caller", CalleeName: "Widget", Confidence: 0.9, Reason: "direct"},
	}
	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Errorf("expected 1 resolved call, got %d", count)
	}

	callerNode := graph.FindNodeByID(g, "func:caller")
	edges := graph.GetOutgoingEdges(callerNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	target := edges[0].GetTo()
	// Should prefer the Function node over the Class node.
	if !target.HasLabel(string(graph.LabelFunction)) {
		t.Error("expected target to be a Function node, not Class")
	}
}

// ---------------------------------------------------------------------------
// Gap 4: Tiered call resolution — same-file beats imports beats global
// ---------------------------------------------------------------------------

func TestResolveCalls_TieredSameFilePriority(t *testing.T) {
	g := lpg.NewGraph()

	// Caller is in "src/main.go".
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:caller", Name: "caller"},
		FilePath:      "src/main.go",
	})

	// Two candidates for "helper": one in same file, one in a different file.
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper_local", Name: "helper"},
		FilePath:      "src/main.go",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper_remote", Name: "helper"},
		FilePath:      "src/other.go",
	})

	calls := []CallInfo{
		{CallerNodeID: "func:caller", CalleeName: "helper", CallerFilePath: "src/main.go", Confidence: 0.9, Reason: "test"},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Fatalf("expected 1 resolved call, got %d", count)
	}

	callerNode := graph.FindNodeByID(g, "func:caller")
	edges := graph.GetOutgoingEdges(callerNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "func:helper_local" {
		t.Errorf("expected same-file target func:helper_local, got %s", targetID)
	}
}

func TestResolveCalls_TieredImportPriority(t *testing.T) {
	g := lpg.NewGraph()

	// Caller is in "src/main.go".
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:caller", Name: "caller"},
		FilePath:      "src/main.go",
	})

	// File node for main.go.
	mainFile := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/main.go", Name: "main.go"},
		FilePath:      "src/main.go",
	})

	// Two candidates for "process": one in imported file, one globally.
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:process_util", Name: "process"},
		FilePath:      "src/utils.go",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:process_shared", Name: "process"},
		FilePath:      "src/shared.go",
	})

	// utils.go file node.
	utilsFile := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:src/utils.go", Name: "utils.go"},
		FilePath:      "src/utils.go",
	})

	// Add IMPORTS edge: main.go → utils.go.
	graph.AddEdge(g, mainFile, utilsFile, graph.RelImports, nil)

	calls := []CallInfo{
		{CallerNodeID: "func:caller", CalleeName: "process", CallerFilePath: "src/main.go", Confidence: 0.9, Reason: "test"},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Fatalf("expected 1 resolved call, got %d", count)
	}

	callerNode := graph.FindNodeByID(g, "func:caller")
	edges := graph.GetOutgoingEdges(callerNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "func:process_util" {
		t.Errorf("expected imported-file target func:process_util, got %s", targetID)
	}
}

func TestResolveCalls_TieredReceiverType(t *testing.T) {
	g := lpg.NewGraph()

	// Caller.
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:caller", Name: "caller"},
		FilePath:      "src/main.go",
	})

	// Two methods named "Run": one on Service, one on Worker.
	serviceMethod := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Service.Run", Name: "Run"},
		FilePath:      "src/service.go",
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Worker.Run", Name: "Run"},
		FilePath:      "src/worker.go",
	})

	// Service class with HAS_METHOD edge to Service.Run.
	serviceClass := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Service", Name: "Service"},
		FilePath:      "src/service.go",
	})
	graph.AddEdge(g, serviceClass, serviceMethod, graph.RelHasMethod, nil)

	calls := []CallInfo{
		{CallerNodeID: "func:caller", CalleeName: "Run", CallerFilePath: "src/main.go", ReceiverType: "Service", Confidence: 0.9, Reason: "test"},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Fatalf("expected 1 resolved call, got %d", count)
	}

	callerNode := graph.FindNodeByID(g, "func:caller")
	edges := graph.GetOutgoingEdges(callerNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "method:Service.Run" {
		t.Errorf("expected receiver-matched target method:Service.Run, got %s", targetID)
	}
}

func TestResolveCalls_AliasedImportResolution(t *testing.T) {
	// Simulates: import { User as U } from "./user"
	// Code calls: U() — the graph has a Function named "User", not "U".
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:caller", Name: "main"},
		FilePath:      "src/app.ts",
	})
	graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:User", Name: "User"},
		FilePath:      "src/user.ts",
	})

	calls := []CallInfo{
		{
			CallerNodeID:   "func:caller",
			CalleeName:     "U",    // alias used in source code
			OriginalName:   "User", // original name from import binding
			CallerFilePath: "src/app.ts",
			Confidence:     0.8,
			Reason:         "aliased import",
		},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Fatalf("expected 1 resolved call via alias, got %d", count)
	}

	callerNode := graph.FindNodeByID(g, "func:caller")
	edges := graph.GetOutgoingEdges(callerNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "class:User" {
		t.Errorf("expected alias-resolved target class:User, got %s", targetID)
	}
}

func TestResolveCalls_AliasedReceiverType(t *testing.T) {
	// Simulates: import { UserService as US } from "./service"
	// Code calls: us.lookup() — receiver "us" has type resolved to "UserService" via alias map.
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:caller", Name: "main"},
		FilePath:      "src/app.ts",
	})

	// Two methods named "lookup": one on UserService, one on ProductService.
	lookupOnUser := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:UserService.lookup", Name: "lookup"},
		FilePath:      "src/service.ts",
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:ProductService.lookup", Name: "lookup"},
		FilePath:      "src/product.ts",
	})

	// UserService class with HAS_METHOD edge.
	userServiceClass := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:UserService", Name: "UserService"},
		FilePath:      "src/service.ts",
	})
	graph.AddEdge(g, userServiceClass, lookupOnUser, graph.RelHasMethod, nil)

	calls := []CallInfo{
		{
			CallerNodeID:   "func:caller",
			CalleeName:     "lookup",
			CallerFilePath: "src/app.ts",
			ReceiverType:   "UserService", // Pipeline resolved alias "US" → "UserService"
			Confidence:     0.8,
			Reason:         "aliased receiver",
		},
	}

	count := ResolveCalls(g, calls)
	if count != 1 {
		t.Fatalf("expected 1 resolved call, got %d", count)
	}

	callerNode := graph.FindNodeByID(g, "func:caller")
	edges := graph.GetOutgoingEdges(callerNode, graph.RelCalls)
	if len(edges) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(edges))
	}

	targetID := graph.GetStringProp(edges[0].GetTo(), graph.PropID)
	if targetID != "method:UserService.lookup" {
		t.Errorf("expected receiver-matched target method:UserService.lookup, got %s", targetID)
	}
}

func TestResolveCalls_CallerNotFound(t *testing.T) {
	g := setupCallGraph()

	calls := []CallInfo{
		{CallerNodeID: "nonexistent", CalleeName: "helper", Confidence: 0.9, Reason: "direct"},
	}
	count := ResolveCalls(g, calls)
	if count != 0 {
		t.Errorf("expected 0 resolved calls when caller not found, got %d", count)
	}
}

func TestResolveCalls_EmptyCallList(t *testing.T) {
	g := setupCallGraph()
	count := ResolveCalls(g, nil)
	if count != 0 {
		t.Errorf("expected 0 for nil call list, got %d", count)
	}
}
