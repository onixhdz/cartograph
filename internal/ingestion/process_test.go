package ingestion

import (
	"fmt"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

func boolPtr(b bool) *bool { return &b }

func TestFindEntryPoints_NoIncomingCalls(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)

	eps := FindEntryPoints(g)
	if len(eps) != 1 {
		t.Fatalf("expected 1 entry point, got %d", len(eps))
	}
	epID := graph.GetStringProp(eps[0], graph.PropID)
	if epID != "func:A" {
		t.Errorf("expected entry point func:A, got %s", epID)
	}
}

func TestFindEntryPoints_FileNodeNotIncluded(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:a.go", Name: "a.go"},
		FilePath:      "a.go",
		Language:      "go",
		Size:          100,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})

	eps := FindEntryPoints(g)
	if len(eps) != 1 {
		t.Fatalf("expected 1 entry point (only functions), got %d", len(eps))
	}
	epID := graph.GetStringProp(eps[0], graph.PropID)
	if epID != "func:main" {
		t.Errorf("expected func:main, got %s", epID)
	}
}

func TestDetectProcesses_SingleEntryNoCalls(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})

	count := DetectProcesses(g, ProcessOptions{MinSteps: 1})
	if count != 1 {
		t.Errorf("expected 1 process, got %d", count)
	}

	processNode := graph.FindNodeByID(g, "process:func:main-flow")
	if processNode == nil {
		t.Fatal("expected process node to exist")
	}

	stepCount := graph.GetIntProp(processNode, graph.PropStepCount)
	if stepCount != 1 {
		t.Errorf("expected step count 1, got %d", stepCount)
	}
}

func TestDetectProcesses_LinearChain(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go",
		StartLine:     1,
		EndLine:       10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnB, fnC, graph.RelCalls, nil)

	count := DetectProcesses(g, ProcessOptions{MinSteps: 1})
	if count != 1 {
		t.Errorf("expected 1 process, got %d", count)
	}

	processNode := graph.FindNodeByID(g, "process:func:A-flow")
	if processNode == nil {
		t.Fatal("expected process node to exist")
	}

	stepCount := graph.GetIntProp(processNode, graph.PropStepCount)
	if stepCount != 3 {
		t.Errorf("expected step count 3, got %d", stepCount)
	}

	for _, fn := range []*lpg.Node{fnA, fnB, fnC} {
		edges := graph.GetOutgoingEdges(fn, graph.RelStepInProcess)
		if len(edges) != 1 {
			fnID := graph.GetStringProp(fn, graph.PropID)
			t.Errorf("expected 1 STEP_IN_PROCESS edge from %s, got %d", fnID, len(edges))
		}
	}
}

func TestDetectProcesses_MaxDepthLimits(t *testing.T) {
	g := lpg.NewGraph()

	// Create chain: A -> B -> C -> D -> E
	prev := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	names := []string{"B", "C", "D", "E"}
	for _, name := range names {
		fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: "func:" + name, Name: name},
			FilePath:      name + ".go",
			StartLine:     1,
			EndLine:       10,
		})
		graph.AddEdge(g, prev, fn, graph.RelCalls, nil)
		prev = fn
	}

	count := DetectProcesses(g, ProcessOptions{MaxDepth: 2, MinSteps: 1})
	if count != 1 {
		t.Errorf("expected 1 process, got %d", count)
	}

	processNode := graph.FindNodeByID(g, "process:func:A-flow")
	if processNode == nil {
		t.Fatal("expected process node to exist")
	}

	// With MaxDepth=2, BFS visits: A(depth 0), B(depth 1), C(depth 2).
	// D is at depth 3 which would be queued from C, but C is at depth=MaxDepth so no children.
	stepCount := graph.GetIntProp(processNode, graph.PropStepCount)
	if stepCount != 3 {
		t.Errorf("expected step count 3 (A, B, C), got %d", stepCount)
	}
}

func TestDetectProcesses_Branching(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnD := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:D", Name: "D"},
		FilePath:      "d.go",
		StartLine:     1,
		EndLine:       10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnA, fnC, graph.RelCalls, nil)
	graph.AddEdge(g, fnA, fnD, graph.RelCalls, nil)

	count := DetectProcesses(g, ProcessOptions{MaxBranching: 2, MinSteps: 1})
	if count != 1 {
		t.Errorf("expected 1 process, got %d", count)
	}

	processNode := graph.FindNodeByID(g, "process:func:A-flow")
	if processNode == nil {
		t.Fatal("expected process node to exist")
	}

	// At depth 0 (entry point A), the branch limit is doubled (2*2=4),
	// so all 3 callees are followed → step count = 4 (A + B + C + D).
	stepCount := graph.GetIntProp(processNode, graph.PropStepCount)
	if stepCount != 4 {
		t.Errorf("expected step count 4 (A + 3 branches, doubled limit at depth 0), got %d", stepCount)
	}
	_ = fnB
	_ = fnC
	_ = fnD
}

func TestDetectProcesses_MultipleEntryPoints(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:init", Name: "init"},
		FilePath:      "init.go",
		StartLine:     1,
		EndLine:       10,
	})

	count := DetectProcesses(g, ProcessOptions{MinSteps: 1})
	if count != 2 {
		t.Errorf("expected 2 processes, got %d", count)
	}
}

func TestDetectProcesses_HeuristicLabels(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		// Application lifecycle.
		{"main", "Application entry point"},
		{"app.main", "Application entry point"},
		{"initConfig", "Initialization"},
		{"bootstrapApp", "Initialization"},
		{"setupRoutes", "Initialization"},
		{"shutdownServer", "Shutdown"},
		{"cleanupResources", "Shutdown"},
		{"teardownDB", "Shutdown"},
		{"disposeContext", "Shutdown"},
		{"closeConn", "Shutdown"},
		// Test.
		{"TestSomething", "Test flow"},
		{"BenchmarkSort", "Test flow"},
		// Server / networking.
		{"serveHTTP", "Server"},
		{"listenAndServe", "Server"},
		{"acceptConnections", "Server"},
		{"handleRequest", "Request handler"},
		{"userHandler", "Request handler"},
		{"apiEndpoint", "Request handler"},
		{"authMiddleware", "Request handler"},
		{"routeRequest", "Router"},
		{"dispatchEvent", "Router"},
		// CRUD operations.
		{"createUser", "Create operation"},
		{"insertRecord", "Create operation"},
		{"addItem", "Create operation"},
		{"newServer", "Server"},
		{"updateProfile", "Update operation"},
		{"modifyEntry", "Update operation"},
		{"patchConfig", "Update operation"},
		{"deleteUser", "Delete operation"},
		{"removeEntry", "Delete operation"},
		{"destroySession", "Delete operation"},
		{"getUser", "Read operation"},
		{"fetchData", "Read operation"},
		{"loadConfig", "Read operation"},
		{"findByID", "Read operation"},
		{"lookupToken", "Read operation"},
		{"queryRecords", "Read operation"},
		{"listUsers", "List operation"},
		{"scanTable", "List operation"},
		// Processing / transformation.
		{"processEval", "Processing"},
		{"executeJob", "Processing"},
		{"runWorker", "Processing"},
		{"evalExpression", "Processing"},
		{"parseConfig", "Parser"},
		{"decodeJSON", "Parser"},
		{"unmarshalProto", "Parser"},
		{"formatOutput", "Serializer"},
		{"encodeResponse", "Serializer"},
		{"marshalJSON", "Serializer"},
		{"renderTemplate", "Serializer"},
		{"convertToMap", "Transformer"},
		{"transformData", "Transformer"},
		{"copyNode", "Copy operation"},
		{"cloneState", "Copy operation"},
		{"mergeResults", "Merge operation"},
		{"combineOutputs", "Merge operation"},
		// Validation / auth.
		{"validateInput", "Validation"},
		{"checkPermission", "Validation"},
		{"verifyToken", "Validation"},
		{"authenticateUser", "Authentication"},
		{"loginHandler", "Request handler"},
		// Coordination.
		{"syncState", "Synchronization"},
		{"reconcileDesired", "Synchronization"},
		{"replicateLog", "Synchronization"},
		{"scheduleEval", "Processing"},
		{"planAllocation", "Scheduler"},
		{"allocateResources", "Scheduler"},
		{"watchChanges", "Watcher"},
		{"monitorHealth", "Watcher"},
		{"pollStatus", "Watcher"},
		{"subscribeEvents", "Watcher"},
		{"emitEvent", "Event emitter"},
		{"publishMessage", "Event emitter"},
		{"notifySubscribers", "Watcher"},
		{"logRequest", "Observability"},
		{"recordMetric", "Observability"},
		// Registration / config.
		{"registerPlugin", "Registration"},
		{"bindAddress", "Create operation"},
		{"wireHandlers", "Request handler"},
		{"configureServer", "Server"},
		// Migration / lifecycle.
		{"migrateSchema", "Migration"},
		{"upgradeVersion", "Migration"},
		{"startServer", "Server"},
		{"stopWorker", "Lifecycle"},
		{"restartService", "Lifecycle"},
		// Fallback.
		{"doWork", "Execution flow"},
		{"calculate", "Execution flow"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := graph.HeuristicLabel(tc.name)
			if got != tc.expected {
				t.Errorf("HeuristicLabel(%q) = %q, want %q", tc.name, got, tc.expected)
			}
		})
	}
}

func TestDetectProcesses_EmptyGraph(t *testing.T) {
	g := lpg.NewGraph()
	count := DetectProcesses(g, ProcessOptions{})
	if count != 0 {
		t.Errorf("expected 0 processes for empty graph, got %d", count)
	}
}

func TestDetectProcesses_MinStepsFilter(t *testing.T) {
	g := lpg.NewGraph()

	// Create a single function with no calls (1 step)
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:solo", Name: "solo"},
		FilePath:      "solo.go",
		StartLine:     1,
		EndLine:       10,
	})

	// Create a chain: A -> B -> C (3 steps)
	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:chainA", Name: "chainA"},
		FilePath:      "chain.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:chainB", Name: "chainB"},
		FilePath:      "chain.go",
		StartLine:     11,
		EndLine:       20,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:chainC", Name: "chainC"},
		FilePath:      "chain.go",
		StartLine:     21,
		EndLine:       30,
	})
	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnB, fnC, graph.RelCalls, nil)

	count := DetectProcesses(g, ProcessOptions{MinSteps: 3})
	if count != 1 {
		t.Errorf("expected 1 process (MinSteps=3 filters solo), got %d", count)
	}
}

func TestDetectProcesses_MaxProcessesLimit(t *testing.T) {
	g := lpg.NewGraph()

	for i := range 5 {
		name := string(rune('A' + i))
		graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: "func:" + name, Name: name},
			FilePath:      name + ".go",
			StartLine:     1,
			EndLine:       10,
		})
	}

	count := DetectProcesses(g, ProcessOptions{MaxProcesses: 3, MinSteps: 1})
	if count != 3 {
		t.Errorf("expected 3 processes (MaxProcesses=3), got %d", count)
	}
}

func TestDetectProcesses_ExcludeTestFiles(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:TestSomething", Name: "TestSomething"},
		FilePath:      "main_test.go",
		StartLine:     1,
		EndLine:       10,
	})

	count := DetectProcesses(g, ProcessOptions{MinSteps: 1})
	if count != 1 {
		t.Errorf("expected 1 process (test file excluded by default), got %d", count)
	}

	g2 := lpg.NewGraph()
	graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:TestSomething", Name: "TestSomething"},
		FilePath:      "main_test.go",
		StartLine:     1,
		EndLine:       10,
	})
	count2 := DetectProcesses(g2, ProcessOptions{ExcludeTests: boolPtr(false), MinSteps: 1})
	if count2 != 2 {
		t.Errorf("expected 2 processes with ExcludeTests=false, got %d", count2)
	}
}

func TestDetectProcesses_LowConfidenceFiltering(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go",
		StartLine:     1,
		EndLine:       10,
	})

	// A -> B (high confidence)
	graph.AddTypedEdge(g, fnA, fnB, graph.EdgeProps{
		Type:       graph.RelCalls,
		Confidence: 0.9,
	})
	// A -> C (low confidence)
	graph.AddTypedEdge(g, fnA, fnC, graph.EdgeProps{
		Type:       graph.RelCalls,
		Confidence: 0.3,
	})

	result := DetectProcessesDetailed(g, ProcessOptions{MinConfidence: 0.5, MinSteps: 1})
	if result.ProcessCount != 1 {
		t.Fatalf("expected 1 process, got %d", result.ProcessCount)
	}
	if result.Processes[0].StepCount != 2 {
		t.Errorf("expected 2 steps (A + B, C filtered), got %d", result.Processes[0].StepCount)
	}
}

func TestDetectProcesses_CrossCommunity(t *testing.T) {
	g := lpg.NewGraph()

	comm1 := graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "community:1", Name: "Backend"},
	})
	comm2 := graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "community:2", Name: "Frontend"},
	})

	// Create a chain: A (Backend) -> B (Frontend)
	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnA, comm1, graph.RelMemberOf, nil)
	graph.AddEdge(g, fnB, comm2, graph.RelMemberOf, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})
	if result.ProcessCount != 1 {
		t.Fatalf("expected 1 process, got %d", result.ProcessCount)
	}
	if !result.Processes[0].CrossCommunity {
		t.Error("expected process to be marked as cross-community")
	}
	if len(result.Processes[0].Communities) != 2 {
		t.Errorf("expected 2 communities, got %d", len(result.Processes[0].Communities))
	}
}

func TestDetectProcesses_ProgressCallback(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:init", Name: "init"},
		FilePath:      "init.go",
		StartLine:     1,
		EndLine:       10,
	})

	var progressCalls []struct{ current, total int }
	DetectProcesses(g, ProcessOptions{
		MinSteps: 1,
		OnProgress: func(current, total int) {
			progressCalls = append(progressCalls, struct{ current, total int }{current, total})
		},
	})

	if len(progressCalls) != 2 {
		t.Errorf("expected 2 progress callbacks, got %d", len(progressCalls))
	}
	if len(progressCalls) > 0 && progressCalls[0].total != 2 {
		t.Errorf("expected total=2, got %d", progressCalls[0].total)
	}
}

func TestDetectProcessesDetailed_ReturnsStructuredResult(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:handler", Name: "handler"},
		FilePath:      "handler.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})
	if result.ProcessCount != 1 {
		t.Fatalf("expected 1 process, got %d", result.ProcessCount)
	}
	if result.TotalSteps != 2 {
		t.Errorf("expected 2 total steps, got %d", result.TotalSteps)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process info, got %d", len(result.Processes))
	}
	p := result.Processes[0]
	if p.Name != "main-flow" {
		t.Errorf("expected process name 'main-flow', got %q", p.Name)
	}
	if p.EntryPoint != "func:main" {
		t.Errorf("expected entry point 'func:main', got %q", p.EntryPoint)
	}
	if p.HeuristicLabel != "Application entry point" {
		t.Errorf("expected heuristic label 'Application entry point', got %q", p.HeuristicLabel)
	}
	if p.StepCount != 2 {
		t.Errorf("expected 2 steps, got %d", p.StepCount)
	}
}

func TestQualifiedFlowName(t *testing.T) {
	tests := []struct {
		funcName string
		filePath string
		expected string
	}{
		// Root-level files: no prefix
		{"main", "main.go", "main-flow"},
		{"init", "init.go", "init-flow"},
		{"handler", "handler.py", "handler-flow"},
		// Empty path: no prefix
		{"foo", "", "foo-flow"},
		// Single subdirectory: uses directory as prefix
		{"Copy", "allocrunner/copy.go", "allocrunner.Copy-flow"},
		{"Copy", "taskrunner/copy.go", "taskrunner.Copy-flow"},
		{"Info", "client/info.go", "client.Info-flow"},
		// Deep path: uses immediate parent
		{"Leader", "nomad/server/leader.go", "server.Leader-flow"},
		{"Start", "internal/service/server.go", "service.Start-flow"},
		// Works across languages
		{"render", "src/components/App.tsx", "components.render-flow"},
		{"__init__", "mypackage/utils/helpers.py", "utils.__init__-flow"},
		{"new", "src/lib/parser.rs", "lib.new-flow"},
	}

	for _, tt := range tests {
		t.Run(tt.funcName+"_"+tt.filePath, func(t *testing.T) {
			got := qualifiedFlowName(tt.funcName, tt.filePath)
			if got != tt.expected {
				t.Errorf("qualifiedFlowName(%q, %q) = %q, want %q", tt.funcName, tt.filePath, got, tt.expected)
			}
		})
	}
}

func TestDetectProcesses_NamespacedFlowNames(t *testing.T) {
	g := lpg.NewGraph()

	// Two different types both have a Copy method in different packages.
	fnA := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:allocrunner.Copy", Name: "Copy"},
		FilePath:      "client/allocrunner/copy.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "client/allocrunner/helper.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)

	fnC := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:taskrunner.Copy", Name: "Copy"},
		FilePath:      "client/taskrunner/copy.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnD := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:util", Name: "util"},
		FilePath:      "client/taskrunner/util.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddEdge(g, fnC, fnD, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})
	if result.ProcessCount != 2 {
		t.Fatalf("expected 2 processes, got %d", result.ProcessCount)
	}

	names := make(map[string]bool)
	for _, p := range result.Processes {
		names[p.Name] = true
	}
	if !names["allocrunner.Copy-flow"] {
		t.Error("expected 'allocrunner.Copy-flow' process")
	}
	if !names["taskrunner.Copy-flow"] {
		t.Error("expected 'taskrunner.Copy-flow' process")
	}
}

func TestDetectProcesses_CallerCount(t *testing.T) {
	g := lpg.NewGraph()

	// A calls B and C; X and Y call B externally (not in the BFS tree).
	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnX := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:X", Name: "X"},
		FilePath:      "x.go",
		StartLine:     1,
		EndLine:       10,
	})
	fnY := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Y", Name: "Y"},
		FilePath:      "y.go",
		StartLine:     1,
		EndLine:       10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnA, fnC, graph.RelCalls, nil)
	graph.AddEdge(g, fnX, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnY, fnB, graph.RelCalls, nil)
	_ = fnX
	_ = fnY

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})
	var aFlow *ProcessInfo
	for i := range result.Processes {
		if result.Processes[i].EntryPoint == "func:A" {
			aFlow = &result.Processes[i]
			break
		}
	}
	if aFlow == nil {
		t.Fatal("expected A-flow process")
	}

	// External callers of B = {X, Y}; A is in visited set so not counted.
	if aFlow.CallerCount != 2 {
		t.Errorf("expected CallerCount=2 (X and Y call B externally), got %d", aFlow.CallerCount)
	}

	processNode := graph.FindNodeByID(g, "process:func:A-flow")
	if processNode == nil {
		t.Fatal("expected process node to exist")
	}
	storedCC := graph.GetIntProp(processNode, graph.PropCallerCount)
	if storedCC != 2 {
		t.Errorf("expected stored callerCount=2, got %d", storedCC)
	}
}

func TestDetectProcesses_ImportanceScore(t *testing.T) {
	g := lpg.NewGraph()

	// Build: A → B → C, with external callers X, Y calling B.
	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1, EndLine: 10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1, EndLine: 10,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:C", Name: "C"},
		FilePath:      "c.go",
		StartLine:     1, EndLine: 10,
	})
	fnX := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:X", Name: "X"},
		FilePath:      "x.go",
		StartLine:     1, EndLine: 10,
	})
	fnY := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Y", Name: "Y"},
		FilePath:      "y.go",
		StartLine:     1, EndLine: 10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnB, fnC, graph.RelCalls, nil)
	graph.AddEdge(g, fnX, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnY, fnB, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	var aFlow *ProcessInfo
	for i := range result.Processes {
		if result.Processes[i].EntryPoint == "func:A" {
			aFlow = &result.Processes[i]
			break
		}
	}
	if aFlow == nil {
		t.Fatal("expected A-flow process")
	}

	// With additive reach formula:
	// effectiveCallers = 0.577, effectiveSteps = 2.386, rawSteps = 3.
	// callerSignal = ln(1.577) ≈ 0.456, reachSignal = 0.3*ln(4) ≈ 0.416.
	// base = (0.456 + 0.416) * ln(3.386) ≈ 1.063.
	// exclusivity = 2.386/3 = 0.795. bonus = 1 + 0.3*sqrt(0.795) ≈ 1.268.
	// importance ≈ 1.063 * 1.268 ≈ 1.35
	if aFlow.Importance < 1.30 || aFlow.Importance > 1.40 {
		t.Errorf("expected importance ≈ 1.35, got %.4f", aFlow.Importance)
	}

	// Verify stored on graph node too.
	processNode := graph.FindNodeByID(g, "process:func:A-flow")
	if processNode == nil {
		t.Fatal("expected process node")
	}
	storedImp := graph.GetFloat64Prop(processNode, graph.PropImportance)
	if storedImp != aFlow.Importance {
		t.Errorf("graph node importance %.4f != ProcessInfo importance %.4f", storedImp, aFlow.Importance)
	}

	// A single-step flow with no external callers should have importance = 0.
	// (ln(1+0) * ln(1+1) = 0 * 0.693 = 0)
	// Add an isolated function Z with no calls in or out.
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Z", Name: "Z"},
		FilePath:      "z.go",
		StartLine:     1, EndLine: 10,
	})
	result2 := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})
	var zFlow *ProcessInfo
	for i := range result2.Processes {
		if result2.Processes[i].EntryPoint == "func:Z" {
			zFlow = &result2.Processes[i]
			break
		}
	}
	if zFlow == nil {
		t.Fatal("expected Z-flow process")
	}
	// Z: callerSignal=0, reachSignal=0.3*ln(2)≈0.208. base=(0+0.208)*ln(2)≈0.144.
	// exclusivity=1.0, bonus=1.3. importance ≈ 0.144 * 1.3 ≈ 0.19.
	if zFlow.Importance < 0.15 || zFlow.Importance > 0.22 {
		t.Errorf("expected importance ≈ 0.19 for 1-step flow with no callers, got %.4f", zFlow.Importance)
	}
}

func TestDetectProcesses_ImportanceCrossCommunityBoost(t *testing.T) {
	g := lpg.NewGraph()

	comm1 := graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "community:1", Name: "Backend"},
	})
	comm2 := graph.AddCommunityNode(g, graph.CommunityProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "community:2", Name: "Frontend"},
	})

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "a.go",
		StartLine:     1, EndLine: 10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "b.go",
		StartLine:     1, EndLine: 10,
	})
	fnExt := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Ext", Name: "Ext"},
		FilePath:      "ext.go",
		StartLine:     1, EndLine: 10,
	})

	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)
	graph.AddEdge(g, fnExt, fnB, graph.RelCalls, nil) // external caller

	// A in Backend, B in Frontend → cross-community
	graph.AddEdge(g, fnA, comm1, graph.RelMemberOf, nil)
	graph.AddEdge(g, fnB, comm2, graph.RelMemberOf, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	var aFlow *ProcessInfo
	for i := range result.Processes {
		if result.Processes[i].EntryPoint == "func:A" {
			aFlow = &result.Processes[i]
			break
		}
	}
	if aFlow == nil {
		t.Fatal("expected A-flow process")
	}
	if !aFlow.CrossCommunity {
		t.Fatal("expected cross-community")
	}

	// Additive reach: callerSignal=0.303, reachSignal=0.3*ln(3)≈0.330.
	// base = (0.303+0.330)*ln(2.794) ≈ 0.651. exclusivity bonus ≈ 1.284.
	// withExclusivity ≈ 0.835. With 1.5x cross-community: ≈ 1.25.
	if aFlow.Importance < 1.20 || aFlow.Importance > 1.30 {
		t.Errorf("expected importance ≈ 1.25 (with 1.5x cross-community bonus), got %.4f", aFlow.Importance)
	}

	baseImportance := aFlow.Importance / 1.5
	if baseImportance < 0.79 || baseImportance > 0.87 {
		t.Errorf("base importance (without cross-community) should be ≈ 0.83, got %.4f", baseImportance)
	}
}

// TestDetectProcesses_SharedCallerNormalization verifies that when multiple
// flows share the same deep steps, each step's callers are weighted by
// 1/sqrt(flowShareCount) instead of 1. The sqrt dampening preserves
// architectural centrality signal while still reducing inflation from
// shared infrastructure (e.g., RPC dispatch).
func TestDetectProcesses_SharedCallerNormalization(t *testing.T) {
	g := lpg.NewGraph()

	// 3 entry points sharing dispatch → auth.
	ep1 := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:EP1", Name: "EP1"},
		FilePath:      "pkg/ep1.go", StartLine: 1, EndLine: 10,
	})
	ep2 := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:EP2", Name: "EP2"},
		FilePath:      "pkg/ep2.go", StartLine: 1, EndLine: 10,
	})
	ep3 := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:EP3", Name: "EP3"},
		FilePath:      "pkg/ep3.go", StartLine: 1, EndLine: 10,
	})
	fnDispatch := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:dispatch", Name: "dispatch"},
		FilePath:      "pkg/dispatch.go", StartLine: 1, EndLine: 10,
	})
	fnAuth := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:auth", Name: "auth"},
		FilePath:      "pkg/auth.go", StartLine: 1, EndLine: 10,
	})

	graph.AddEdge(g, ep1, fnDispatch, graph.RelCalls, nil)
	graph.AddEdge(g, ep2, fnDispatch, graph.RelCalls, nil)
	graph.AddEdge(g, ep3, fnDispatch, graph.RelCalls, nil)
	graph.AddEdge(g, fnDispatch, fnAuth, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	// EP-flow with additive reach: importance ≈ 1.35 (same as A-flow).
	for _, p := range result.Processes {
		if p.Importance < 1.30 || p.Importance > 1.40 {
			t.Errorf("flow %s: expected importance ≈ 1.35, got %.4f", p.Name, p.Importance)
		}
		if p.CallerCount != 2 {
			t.Errorf("flow %s: expected raw callerCount=2, got %d", p.Name, p.CallerCount)
		}
	}

	// Sanity: with only 1 flow (no sharing), normalization has no effect
	// and the formula produces the same result as the old formula.
	g2 := lpg.NewGraph()
	solo := graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:solo", Name: "solo"},
		FilePath:      "pkg/solo.go", StartLine: 1, EndLine: 10,
	})
	soloCallee := graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:callee", Name: "callee"},
		FilePath:      "pkg/callee.go", StartLine: 1, EndLine: 10,
	})
	extCaller := graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:ext", Name: "ext"},
		FilePath:      "pkg/ext.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g2, solo, soloCallee, graph.RelCalls, nil)
	graph.AddEdge(g2, extCaller, soloCallee, graph.RelCalls, nil)

	r2 := DetectProcessesDetailed(g2, ProcessOptions{MinSteps: 1})
	var soloFlow *ProcessInfo
	for i := range r2.Processes {
		if r2.Processes[i].EntryPoint == "func:solo" {
			soloFlow = &r2.Processes[i]
			break
		}
	}
	if soloFlow == nil {
		t.Fatal("expected solo-flow")
	}
	// solo-flow with additive reach: importance ≈ 0.83 (same as cross-community base).
	if soloFlow.Importance < 0.79 || soloFlow.Importance > 0.87 {
		t.Errorf("solo-flow: expected importance ≈ 0.83, got %.4f", soloFlow.Importance)
	}
}

// TestQualifiedFlowNameWithReceiver verifies that Method entry points
// include the receiver type in the flow name.
func TestQualifiedFlowNameWithReceiver(t *testing.T) {
	g := lpg.NewGraph()

	// Struct CSIVolume with method List.
	structNode := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:nomad.CSIVolume", Name: "CSIVolume"},
		FilePath:      "nomad/csi_volume.go", StartLine: 1, EndLine: 50,
	})
	methodNode := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:nomad.CSIVolume.List", Name: "List"},
		FilePath:      "nomad/csi_volume.go", StartLine: 60, EndLine: 80,
	})
	graph.AddEdge(g, structNode, methodNode, graph.RelHasMethod, nil)

	// Method with receiver → pkg.Type.Method-flow
	got := qualifiedFlowNameWithReceiver(methodNode, "List", "nomad/csi_volume.go")
	want := "nomad.CSIVolume.List-flow"
	if got != want {
		t.Errorf("qualifiedFlowNameWithReceiver() = %q, want %q", got, want)
	}

	// Function (not a Method) → falls back to pkg.Func-flow
	fnNode := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:nomad.Leader", Name: "Leader"},
		FilePath:      "nomad/leader.go", StartLine: 1, EndLine: 10,
	})
	got2 := qualifiedFlowNameWithReceiver(fnNode, "Leader", "nomad/leader.go")
	want2 := "nomad.Leader-flow"
	if got2 != want2 {
		t.Errorf("qualifiedFlowNameWithReceiver() for function = %q, want %q", got2, want2)
	}

	// Method without HAS_METHOD edge → falls back to pkg.Method-flow
	orphanMethod := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:orphan.Foo", Name: "Foo"},
		FilePath:      "pkg/foo.go", StartLine: 1, EndLine: 10,
	})
	got3 := qualifiedFlowNameWithReceiver(orphanMethod, "Foo", "pkg/foo.go")
	want3 := "pkg.Foo-flow"
	if got3 != want3 {
		t.Errorf("qualifiedFlowNameWithReceiver() for orphan method = %q, want %q", got3, want3)
	}

	// Method with receiver at root path → Type.Method-flow (no pkg prefix)
	structRoot := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:Server", Name: "Server"},
		FilePath:      "server.go", StartLine: 1, EndLine: 10,
	})
	methodRoot := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Server.Run", Name: "Run"},
		FilePath:      "server.go", StartLine: 20, EndLine: 40,
	})
	graph.AddEdge(g, structRoot, methodRoot, graph.RelHasMethod, nil)
	got4 := qualifiedFlowNameWithReceiver(methodRoot, "Run", "server.go")
	want4 := "Server.Run-flow"
	if got4 != want4 {
		t.Errorf("qualifiedFlowNameWithReceiver() for root method = %q, want %q", got4, want4)
	}
}

// TestDetectProcesses_ReceiverTypeInFlowName verifies end-to-end that
// Method entry points with HAS_METHOD edges produce receiver-typed flow names.
func TestDetectProcesses_ReceiverTypeInFlowName(t *testing.T) {
	g := lpg.NewGraph()

	structNode := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:nomad.CSIVolume", Name: "CSIVolume"},
		FilePath:      "nomad/csi_volume.go", StartLine: 1, EndLine: 50,
	})
	methodList := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:nomad.CSIVolume.List", Name: "List"},
		FilePath:      "nomad/csi_volume.go", StartLine: 60, EndLine: 80,
	})
	graph.AddEdge(g, structNode, methodList, graph.RelHasMethod, nil)

	fnHelper := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "nomad/helpers.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g, methodList, fnHelper, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	var found bool
	for _, p := range result.Processes {
		if p.Name == "nomad.CSIVolume.List-flow" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(result.Processes))
		for i, p := range result.Processes {
			names[i] = p.Name
		}
		t.Errorf("expected 'nomad.CSIVolume.List-flow', got flows: %v", names)
	}
}

// TestDetectProcesses_TestFlowPenalty verifies that entry points in test files
// receive a 0.1× importance penalty so they don't dominate global ranking.
func TestDetectProcesses_TestFlowPenalty(t *testing.T) {
	g := lpg.NewGraph()

	fnProd := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Leader", Name: "Leader"},
		FilePath:      "nomad/leader.go", StartLine: 1, EndLine: 50,
	})
	fnProdCallee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:monitor", Name: "monitor"},
		FilePath:      "nomad/leader.go", StartLine: 60, EndLine: 80,
	})
	graph.AddEdge(g, fnProd, fnProdCallee, graph.RelCalls, nil)

	// External caller into a step (not the entry point) so callerCount > 0.
	extProd := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "nomad/main.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, extProd, fnProdCallee, graph.RelCalls, nil)

	fnTest := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:TestConsul", Name: "TestConsul"},
		FilePath:      "e2e/consul_test.go", StartLine: 1, EndLine: 100,
	})
	fnTestCallee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:setup", Name: "setup"},
		FilePath:      "e2e/consul_test.go", StartLine: 110, EndLine: 130,
	})
	graph.AddEdge(g, fnTest, fnTestCallee, graph.RelCalls, nil)

	// External caller so callerCount > 0.
	extTest := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:TestMain", Name: "TestMain"},
		FilePath:      "e2e/main_test.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, extTest, fnTestCallee, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{
		MinSteps:     1,
		ExcludeTests: boolPtr(false), // include test entry points
	})

	var prodFlow, testFlow *ProcessInfo
	for i := range result.Processes {
		switch result.Processes[i].EntryPoint {
		case "func:Leader":
			prodFlow = &result.Processes[i]
		case "func:TestConsul":
			testFlow = &result.Processes[i]
		}
	}
	if prodFlow == nil || testFlow == nil {
		t.Fatal("expected both Leader-flow and TestConsul-flow")
	}

	// Test flow should have 10% of equivalent production flow importance.
	if testFlow.Importance >= prodFlow.Importance {
		t.Errorf("test flow importance (%.4f) should be less than production flow (%.4f)",
			testFlow.Importance, prodFlow.Importance)
	}
	// Both have same structure (2 steps, 0 callers), so test flow should be ~10% of prod.
	ratio := testFlow.Importance / prodFlow.Importance
	if ratio > 0.15 {
		t.Errorf("test flow importance ratio should be ~0.1, got %.4f", ratio)
	}
}

// TestDetectProcesses_PackageDiversityBonus verifies that flows whose steps
// span many packages receive a higher importance than flows confined to one
// package, reflecting their role as cross-cutting architectural flows.
func TestDetectProcesses_PackageDiversityBonus(t *testing.T) {
	g := lpg.NewGraph()

	// Diverse flow: Plan → SubmitPlan → Apply, each in different packages.
	fnPlan := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Plan", Name: "Plan"},
		FilePath:      "nomad/plan.go", StartLine: 1, EndLine: 50,
	})
	fnSubmit := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:SubmitPlan", Name: "SubmitPlan"},
		FilePath:      "scheduler/submit.go", StartLine: 1, EndLine: 30,
	})
	fnApply := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Apply", Name: "Apply"},
		FilePath:      "state/apply.go", StartLine: 1, EndLine: 20,
	})
	graph.AddEdge(g, fnPlan, fnSubmit, graph.RelCalls, nil)
	graph.AddEdge(g, fnSubmit, fnApply, graph.RelCalls, nil)

	// Narrow flow: Cmd → helper → util, all in the same package.
	fnCmd := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Cmd", Name: "Cmd"},
		FilePath:      "cmd/run.go", StartLine: 1, EndLine: 50,
	})
	fnHelper := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "cmd/helper.go", StartLine: 1, EndLine: 30,
	})
	fnUtil := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:util", Name: "util"},
		FilePath:      "cmd/util.go", StartLine: 1, EndLine: 20,
	})
	graph.AddEdge(g, fnCmd, fnHelper, graph.RelCalls, nil)
	graph.AddEdge(g, fnHelper, fnUtil, graph.RelCalls, nil)

	// External callers so importance isn't zero.
	extA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:extA", Name: "extA"},
		FilePath:      "ext/a.go", StartLine: 1, EndLine: 5,
	})
	extB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:extB", Name: "extB"},
		FilePath:      "ext/b.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, extA, fnSubmit, graph.RelCalls, nil)
	graph.AddEdge(g, extB, fnHelper, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	var planFlow, cmdFlow *ProcessInfo
	for i := range result.Processes {
		switch result.Processes[i].EntryPoint {
		case "func:Plan":
			planFlow = &result.Processes[i]
		case "func:Cmd":
			cmdFlow = &result.Processes[i]
		}
	}
	if planFlow == nil || cmdFlow == nil {
		t.Fatal("expected both Plan-flow and Cmd-flow")
	}

	// Plan spans 3 packages (nomad, scheduler, state) → diversity bonus.
	// Cmd stays in 1 package (cmd) → no bonus.
	// Both have same step count and similar effective callers, so Plan
	// should rank strictly higher.
	if planFlow.Importance <= cmdFlow.Importance {
		t.Errorf("Plan-flow (%.4f) should rank higher than Cmd-flow (%.4f) due to package diversity",
			planFlow.Importance, cmdFlow.Importance)
	}
}

// TestDetectProcesses_FanOutBonus verifies that entry points with high
// direct fan-out (many callees) get an importance boost, reflecting
// their role as orchestrators.
func TestDetectProcesses_FanOutBonus(t *testing.T) {
	g := lpg.NewGraph()

	// High fan-out orchestrator: calls 5 functions directly.
	fnOrch := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Orchestrate", Name: "Orchestrate"},
		FilePath:      "core/orch.go", StartLine: 1, EndLine: 50,
	})
	for i := range 5 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:step%d", i),
				Name: fmt.Sprintf("step%d", i),
			},
			FilePath: fmt.Sprintf("core/step%d.go", i), StartLine: 1, EndLine: 10,
		})
		graph.AddEdge(g, fnOrch, callee, graph.RelCalls, nil)
	}

	// Low fan-out wrapper: calls 1 function directly.
	fnWrap := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Wrap", Name: "Wrap"},
		FilePath:      "util/wrap.go", StartLine: 1, EndLine: 50,
	})
	wrapCallee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:inner", Name: "inner"},
		FilePath:      "util/inner.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g, fnWrap, wrapCallee, graph.RelCalls, nil)

	// External callers so importance isn't zero.
	ext := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:ext", Name: "ext"},
		FilePath:      "ext/e.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, ext, fnOrch, graph.RelCalls, nil)

	extO := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:extO", Name: "extO"},
		FilePath:      "ext/eo.go", StartLine: 1, EndLine: 5,
	})
	extW := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:extW", Name: "extW"},
		FilePath:      "ext/ew.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, extO, fnOrch, graph.RelCalls, nil)
	graph.AddEdge(g, extW, wrapCallee, graph.RelCalls, nil)

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	// ext is the entry point whose flow includes fnOrch and its 5 callees.
	var highFanout, lowFanout float64
	for _, p := range result.Processes {
		if p.StepCount > 3 {
			highFanout = p.Importance
		} else if p.StepCount == 2 {
			lowFanout = p.Importance
		}
	}
	// The flow rooted at ext (which reaches fnOrch and its 5 callees)
	// should outrank the flow rooted at Wrap (1 callee).
	if highFanout > 0 && lowFanout > 0 && highFanout <= lowFanout {
		t.Errorf("high-fanout flow (%.4f) should outrank low-fanout flow (%.4f)",
			highFanout, lowFanout)
	}
}

// TestDetectProcesses_TestCallerExclusion verifies that callers from test
// files are excluded from effectiveCallers. Combined with step exclusivity
// and depth decay, this prevents heavily-tested handler flows from
// outranking core domain flows.
func TestDetectProcesses_TestCallerExclusion(t *testing.T) {
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "pkg/a.go", StartLine: 1, EndLine: 10,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "pkg/b.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)

	fnProd := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:prod", Name: "prod"},
		FilePath:      "pkg/prod.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g, fnProd, fnB, graph.RelCalls, nil)

	for i := range 10 {
		tc := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:Test%d", i),
				Name: fmt.Sprintf("Test%d", i),
			},
			FilePath: "pkg/b_test.go", StartLine: i * 10, EndLine: i*10 + 5,
		})
		graph.AddEdge(g, tc, fnB, graph.RelCalls, nil)
	}

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	var aFlow *ProcessInfo
	for i := range result.Processes {
		if result.Processes[i].EntryPoint == "func:A" {
			aFlow = &result.Processes[i]
			break
		}
	}
	if aFlow == nil {
		t.Fatal("expected A-flow")
	}

	// Raw callerCount includes all callers (prod + 10 tests = 11).
	if aFlow.CallerCount != 11 {
		t.Errorf("expected raw callerCount=11, got %d", aFlow.CallerCount)
	}

	// Build equivalent with only the production caller.
	g2 := lpg.NewGraph()
	fnA2 := graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:A", Name: "A"},
		FilePath:      "pkg/a.go", StartLine: 1, EndLine: 10,
	})
	fnB2 := graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:B", Name: "B"},
		FilePath:      "pkg/b.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g2, fnA2, fnB2, graph.RelCalls, nil)
	fnProd2 := graph.AddSymbolNode(g2, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:prod", Name: "prod"},
		FilePath:      "pkg/prod.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g2, fnProd2, fnB2, graph.RelCalls, nil)

	r2 := DetectProcessesDetailed(g2, ProcessOptions{MinSteps: 1})
	var aFlow2 *ProcessInfo
	for i := range r2.Processes {
		if r2.Processes[i].EntryPoint == "func:A" {
			aFlow2 = &r2.Processes[i]
			break
		}
	}
	if aFlow2 == nil {
		t.Fatal("expected A-flow in reference graph")
	}

	// Test callers are excluded, so importance should match the
	// production-only graph.
	if aFlow.Importance != aFlow2.Importance {
		t.Errorf("test callers should be excluded: with tests (%.4f) != without (%.4f)",
			aFlow.Importance, aFlow2.Importance)
	}
}

// TestFindOrchestrators verifies that functions with few callers but high
// fan-out are detected as orchestrator entry points, while utilities
// (many callers) and low-fanout functions are excluded.
func TestFindOrchestrators(t *testing.T) {
	g := lpg.NewGraph()

	// Orchestrator: 1 caller, 6 callees.
	orch := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:monitorLeadership", Name: "monitorLeadership"},
		FilePath:      "nomad/leader.go", StartLine: 1, EndLine: 100,
	})
	caller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:leaderLoop", Name: "leaderLoop"},
		FilePath:      "nomad/leader.go", StartLine: 110, EndLine: 120,
	})
	graph.AddEdge(g, caller, orch, graph.RelCalls, nil)
	for i := range 6 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:setup%d", i),
				Name: fmt.Sprintf("setup%d", i),
			},
			FilePath: "nomad/leader.go", StartLine: 200 + i*10, EndLine: 209 + i*10,
		})
		graph.AddEdge(g, orch, callee, graph.RelCalls, nil)
	}

	// Utility: 10 callers, 6 callees — too many callers.
	util := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:rpcHelper", Name: "rpcHelper"},
		FilePath:      "nomad/rpc.go", StartLine: 1, EndLine: 20,
	})
	for i := range 10 {
		c := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:rpcCaller%d", i),
				Name: fmt.Sprintf("rpcCaller%d", i),
			},
			FilePath: "nomad/endpoints.go", StartLine: i * 10, EndLine: i*10 + 5,
		})
		graph.AddEdge(g, c, util, graph.RelCalls, nil)
	}
	for i := range 6 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:utilCallee%d", i),
				Name: fmt.Sprintf("utilCallee%d", i),
			},
			FilePath: "nomad/rpc.go", StartLine: 100 + i*10, EndLine: 109 + i*10,
		})
		graph.AddEdge(g, util, callee, graph.RelCalls, nil)
	}

	// Low fan-out: 1 caller, 2 callees — too few callees.
	lowFan := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:wrapper", Name: "wrapper"},
		FilePath:      "nomad/util.go", StartLine: 1, EndLine: 10,
	})
	wrapCaller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, wrapCaller, lowFan, graph.RelCalls, nil)
	for i := range 2 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:wrapCallee%d", i),
				Name: fmt.Sprintf("wrapCallee%d", i),
			},
			FilePath: "nomad/util.go", StartLine: 20 + i*10, EndLine: 29 + i*10,
		})
		graph.AddEdge(g, lowFan, callee, graph.RelCalls, nil)
	}

	// True entry point (no callers) — should NOT appear in orchestrators.
	ep := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:entryMain", Name: "entryMain"},
		FilePath:      "main.go", StartLine: 10, EndLine: 20,
	})

	existingEPs := []*lpg.Node{ep}
	result := findOrchestrators(g, existingEPs, true)

	if len(result) != 1 {
		names := make([]string, len(result))
		for i, n := range result {
			names[i] = graph.GetStringProp(n, graph.PropName)
		}
		t.Fatalf("expected 1 orchestrator, got %d: %v", len(result), names)
	}
	if graph.GetStringProp(result[0], graph.PropName) != "monitorLeadership" {
		t.Errorf("expected monitorLeadership, got %s", graph.GetStringProp(result[0], graph.PropName))
	}
}

// TestFindOrchestrators_TestCallersIgnored verifies that test-file callers
// don't count toward the caller limit.
func TestFindOrchestrators_TestCallersIgnored(t *testing.T) {
	g := lpg.NewGraph()

	orch := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:run", Name: "run"},
		FilePath:      "server/run.go", StartLine: 1, EndLine: 50,
	})
	prodCaller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:newServer", Name: "newServer"},
		FilePath:      "server/server.go", StartLine: 1, EndLine: 10,
	})
	graph.AddEdge(g, prodCaller, orch, graph.RelCalls, nil)

	for i := range 20 {
		tc := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:Test%d", i),
				Name: fmt.Sprintf("Test%d", i),
			},
			FilePath: "server/run_test.go", StartLine: i * 10, EndLine: i*10 + 5,
		})
		graph.AddEdge(g, tc, orch, graph.RelCalls, nil)
	}

	for i := range 7 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:step%d", i),
				Name: fmt.Sprintf("step%d", i),
			},
			FilePath: "server/steps.go", StartLine: i * 20, EndLine: i*20 + 15,
		})
		graph.AddEdge(g, orch, callee, graph.RelCalls, nil)
	}

	result := findOrchestrators(g, nil, true)

	if len(result) != 1 {
		t.Fatalf("expected 1 orchestrator, got %d", len(result))
	}
	if graph.GetStringProp(result[0], graph.PropName) != "run" {
		t.Errorf("expected run, got %s", graph.GetStringProp(result[0], graph.PropName))
	}
}

// TestDetectProcesses_OrchestratorFlows verifies end-to-end that
// orchestrator functions generate their own flows.
func TestDetectProcesses_OrchestratorFlows(t *testing.T) {
	g := lpg.NewGraph()

	// main → monitorLeadership → 6 subsystems
	fnMain := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: "main"},
		FilePath:      "main.go", StartLine: 1, EndLine: 5,
	})
	fnMonitor := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:monitorLeadership", Name: "monitorLeadership"},
		FilePath:      "server/leader.go", StartLine: 1, EndLine: 100,
	})
	graph.AddEdge(g, fnMain, fnMonitor, graph.RelCalls, nil)

	for i := range 6 {
		sub := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:subsystem%d", i),
				Name: fmt.Sprintf("subsystem%d", i),
			},
			FilePath: fmt.Sprintf("server/sub%d.go", i), StartLine: 1, EndLine: 20,
		})
		graph.AddEdge(g, fnMonitor, sub, graph.RelCalls, nil)
	}

	result := DetectProcessesDetailed(g, ProcessOptions{MinSteps: 1})

	flowNames := make(map[string]bool)
	for _, p := range result.Processes {
		flowNames[p.Name] = true
	}

	if !flowNames["main-flow"] {
		t.Error("expected main-flow")
	}
	if !flowNames["server.monitorLeadership-flow"] {
		t.Errorf("expected server.monitorLeadership-flow as orchestrator, got flows: %v", flowNames)
	}
}

// TestFindOrchestrators_LoweredThreshold verifies that functions with exactly
// 4 callees are detected (previously required 5), while 0-caller functions
// are rejected (they should be caught by findEntryPointsFiltered instead).
func TestFindOrchestrators_LoweredThreshold(t *testing.T) {
	g := lpg.NewGraph()

	orch := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:allocSync", Name: "allocSync"},
		FilePath:      "client/client.go", StartLine: 1, EndLine: 40,
	})
	caller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:clientRun", Name: "clientRun"},
		FilePath:      "client/client.go", StartLine: 50, EndLine: 60,
	})
	graph.AddEdge(g, caller, orch, graph.RelCalls, nil)

	for i := range 4 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:syncStep%d", i),
				Name: fmt.Sprintf("syncStep%d", i),
			},
			FilePath: "client/sync.go", StartLine: i * 20, EndLine: i*20 + 15,
		})
		graph.AddEdge(g, orch, callee, graph.RelCalls, nil)
	}

	// 3 callees — below threshold, NOT an orchestrator.
	tooFew := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:smallCoord", Name: "smallCoord"},
		FilePath:      "client/small.go", StartLine: 1, EndLine: 20,
	})
	fewCaller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fewCaller", Name: "fewCaller"},
		FilePath:      "client/small.go", StartLine: 30, EndLine: 40,
	})
	graph.AddEdge(g, fewCaller, tooFew, graph.RelCalls, nil)
	for i := range 3 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:fewCallee%d", i),
				Name: fmt.Sprintf("fewCallee%d", i),
			},
			FilePath: "client/small.go", StartLine: 50 + i*10, EndLine: 59 + i*10,
		})
		graph.AddEdge(g, tooFew, callee, graph.RelCalls, nil)
	}

	// Zero callers, 5 callees — rejected (0-caller = entry point, not orchestrator).
	zeroCaller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:asyncLoop", Name: "asyncLoop"},
		FilePath:      "client/loop.go", StartLine: 1, EndLine: 50,
	})
	for i := range 5 {
		callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{
				ID:   fmt.Sprintf("func:loopStep%d", i),
				Name: fmt.Sprintf("loopStep%d", i),
			},
			FilePath: "client/loop.go", StartLine: 60 + i*10, EndLine: 69 + i*10,
		})
		graph.AddEdge(g, zeroCaller, callee, graph.RelCalls, nil)
	}

	result := findOrchestrators(g, nil, true)

	if len(result) != 1 {
		names := make([]string, len(result))
		for i, n := range result {
			names[i] = graph.GetStringProp(n, graph.PropName)
		}
		t.Fatalf("expected 1 orchestrator (allocSync with 4 callees), got %d: %v", len(result), names)
	}
	if graph.GetStringProp(result[0], graph.PropName) != "allocSync" {
		t.Errorf("expected allocSync, got %s", graph.GetStringProp(result[0], graph.PropName))
	}
}
