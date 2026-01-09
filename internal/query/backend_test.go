package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/cloudprivacylabs/opencypher"

	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/service"
)

const (
	testFuncHandleRequest = "handleRequest"
	testFuncMain          = "main"
	testFuncValidate      = "validate"
	testFileMainGo        = "main.go"
	testNameBob           = "bob"
	testNameCharlie       = "charlie"
)

// buildTestGraph creates a graph with several symbols and relationships.
func buildTestGraph() *lpg.Graph {
	g := lpg.NewGraph()

	// Functions
	fnMain := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:main", Name: testFuncMain},
		FilePath:      testFileMainGo,
		StartLine:     1,
		EndLine:       20,
		Content:       "func main() { handleRequest() }",
	})
	fnHandle := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:handleRequest", Name: testFuncHandleRequest},
		FilePath:      "server.go",
		StartLine:     10,
		EndLine:       30,
		Content:       "func handleRequest(w http.ResponseWriter, r *http.Request) { validate() }",
	})
	fnValidate := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:validate", Name: testFuncValidate},
		FilePath:      "auth.go",
		StartLine:     5,
		EndLine:       15,
		Content:       "func validate(token string) error { }",
	})
	fnHelper := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:helper", Name: "helper"},
		FilePath:      "utils.go",
		StartLine:     1,
		EndLine:       10,
	})

	// Files
	fileMain := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:main.go", Name: testFileMainGo},
		FilePath:      testFileMainGo,
		Language:      "go",
	})
	fileServer := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "file:server.go", Name: "server.go"},
		FilePath:      "server.go",
		Language:      "go",
	})

	// CALLS edges: main -> handleRequest -> validate
	graph.AddEdge(g, fnMain, fnHandle, graph.RelCalls, nil)
	graph.AddEdge(g, fnHandle, fnValidate, graph.RelCalls, nil)

	// IMPORTS edge: server.go imports auth.go
	graph.AddEdge(g, fileServer, fileMain, graph.RelImports, nil)

	// Process: main-flow
	processNode := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps:  graph.BaseNodeProps{ID: "process:main-flow", Name: "main-flow"},
		EntryPoint:     "func:main",
		HeuristicLabel: "Application entry point",
		StepCount:      3,
	})
	graph.AddTypedEdge(g, fnMain, processNode, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 1})
	graph.AddTypedEdge(g, fnHandle, processNode, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 2})
	graph.AddTypedEdge(g, fnValidate, processNode, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 3})

	_ = fnHelper
	return g
}

func TestQuery_NoIndex_FallbackNameSearch(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "handle", Limit: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Definitions) == 0 {
		t.Fatal("expected at least 1 definition for 'handle'")
	}
	found := false
	for _, d := range result.Definitions {
		if d.Name == testFuncHandleRequest {
			found = true
		}
	}
	if !found {
		t.Error("expected handleRequest in definitions")
	}
}

func TestQuery_DefaultLimit(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "a", Limit: 0})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	_ = result
}

func TestQuery_ProcessMembership(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: testFuncMain, Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Processes) == 0 {
		t.Error("expected at least 1 process match")
	}
}

func TestQuery_NoResults(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "zzzznonexistent", Limit: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Definitions) != 0 {
		t.Errorf("expected 0 definitions, got %d", len(result.Definitions))
	}
}

func TestContext_ByName(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncHandleRequest})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.Symbol.Name != testFuncHandleRequest {
		t.Errorf("expected symbol name handleRequest, got %q", result.Symbol.Name)
	}
	if result.Symbol.FilePath != "server.go" {
		t.Errorf("expected filePath server.go, got %q", result.Symbol.FilePath)
	}
	if result.Symbol.Label != "Function" {
		t.Errorf("expected label Function, got %q", result.Symbol.Label)
	}
}

func TestContext_Callers(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncHandleRequest})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(result.Callers) != 1 {
		t.Fatalf("expected 1 caller, got %d", len(result.Callers))
	}
	if result.Callers[0].Name != testFuncMain {
		t.Errorf("expected caller main, got %q", result.Callers[0].Name)
	}
}

func TestContext_Callees(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncHandleRequest})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(result.Callees) != 1 {
		t.Fatalf("expected 1 callee, got %d", len(result.Callees))
	}
	if result.Callees[0].Name != testFuncValidate {
		t.Errorf("expected callee validate, got %q", result.Callees[0].Name)
	}
}

func TestContext_ProcessMembership(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncHandleRequest})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(result.Processes) == 0 {
		t.Error("expected at least 1 process for handleRequest")
	}
}

func TestContext_ByUID(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: "anything", UID: "func:validate"})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.Symbol.Name != testFuncValidate {
		t.Errorf("expected validate (by UID), got %q", result.Symbol.Name)
	}
}

func TestContext_ByFile(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncMain, File: testFileMainGo})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.Symbol.FilePath != testFileMainGo {
		t.Errorf("expected main.go, got %q", result.Symbol.FilePath)
	}
}

func TestContext_NotFound(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	_, err := b.Context(service.ContextRequest{Repo: "test", Name: "nonexistent"})
	if err == nil {
		t.Error("expected error for nonexistent symbol")
	}
}

func TestContext_IncludeContent(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncValidate, Content: true})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.Symbol.Content == "" {
		t.Error("expected content to be included")
	}
}

func TestContext_ExcludeContent(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncValidate, Content: false})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.Symbol.Content != "" {
		t.Error("expected content to be excluded")
	}
}

func TestContext_Depth1_FlatCallees(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncMain, Depth: 1})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.CallTree != nil {
		t.Error("expected no call tree at depth 1")
	}
	if len(result.Callees) != 1 || result.Callees[0].Name != testFuncHandleRequest {
		t.Errorf("expected 1 callee handleRequest, got %v", result.Callees)
	}
}

func TestContext_Depth0_DefaultsToDepth1(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncMain, Depth: 0})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.CallTree != nil {
		t.Error("expected no call tree at depth 0 (defaults to 1)")
	}
	if len(result.Callees) != 1 {
		t.Errorf("expected 1 callee, got %d", len(result.Callees))
	}
}

func TestContext_Depth2_CallTree(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncMain, Depth: 2})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.CallTree == nil {
		t.Fatal("expected call tree at depth 2")
	}
	if result.CallTree.Symbol.Name != testFuncMain {
		t.Errorf("expected root node main, got %q", result.CallTree.Symbol.Name)
	}
	if len(result.CallTree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(result.CallTree.Children))
	}
	child := result.CallTree.Children[0]
	if child.Symbol.Name != testFuncHandleRequest {
		t.Errorf("expected child handleRequest, got %q", child.Symbol.Name)
	}
	if len(child.Children) != 1 || child.Children[0].Symbol.Name != testFuncValidate {
		t.Errorf("expected grandchild validate, got %v", child.Children)
	}
}

func TestContext_Depth2_NoCycles(t *testing.T) {
	g := lpg.NewGraph()
	a := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:a", Name: "a"},
		FilePath:      "a.go", StartLine: 1,
	})
	bNode := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:b", Name: "b"},
		FilePath:      "b.go", StartLine: 1,
	})
	graph.AddEdge(g, a, bNode, graph.RelCalls, nil)
	graph.AddEdge(g, bNode, a, graph.RelCalls, nil)

	b := &Backend{Graph: g}
	result, err := b.Context(service.ContextRequest{Repo: "test", Name: "a", Depth: 5})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.CallTree == nil {
		t.Fatal("expected call tree")
	}
	if len(result.CallTree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(result.CallTree.Children))
	}
	if len(result.CallTree.Children[0].Children) != 0 {
		t.Error("expected cycle to be broken — no grandchildren")
	}
}

func TestContext_Depth3_SpawnsAndDelegates(t *testing.T) {
	g := lpg.NewGraph()
	root := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:root", Name: "root"},
		FilePath:      testFileMainGo, StartLine: 1,
	})
	spawned := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:worker", Name: "worker"},
		FilePath:      "worker.go", StartLine: 1,
	})
	delegated := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:handler", Name: "handler"},
		FilePath:      "handler.go", StartLine: 1,
	})
	graph.AddEdge(g, root, spawned, graph.RelSpawns, nil)
	graph.AddEdge(g, root, delegated, graph.RelDelegatesTo, nil)

	b := &Backend{Graph: g}
	result, err := b.Context(service.ContextRequest{Repo: "test", Name: "root", Depth: 2})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.CallTree == nil {
		t.Fatal("expected call tree")
	}
	if len(result.CallTree.Children) != 2 {
		t.Fatalf("expected 2 children (spawned + delegated), got %d", len(result.CallTree.Children))
	}
	edgeTypes := map[string]bool{}
	for _, c := range result.CallTree.Children {
		edgeTypes[c.EdgeType] = true
	}
	if !edgeTypes["SPAWNS"] || !edgeTypes["DELEGATES_TO"] {
		t.Errorf("expected SPAWNS and DELEGATES_TO edge types, got %v", edgeTypes)
	}
}

// --- Cypher tests ---

func TestCypher_ReadOnlyReturnsResults(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{Repo: "test", Query: "MATCH (n) RETURN n"})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// With opencypher wired, MATCH (n) RETURN n should return rows.
	if len(result.Rows) == 0 {
		t.Error("expected non-empty rows from MATCH (n) RETURN n")
	}
}

func TestCypher_MatchByLabel(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function) RETURN n.name",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should find functions: main, handleRequest, validate, helper.
	if len(result.Rows) < 3 {
		t.Errorf("expected at least 3 Function rows, got %d", len(result.Rows))
	}
}

func TestCypher_MatchByProperty(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function {name: 'validate'}) RETURN n.filePath",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestCypher_WriteBlocked(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	writeQueries := []string{
		"CREATE (n:Node)",
		"MATCH (n) DELETE n",
		"MATCH (n) SET n.x = 1",
		"MERGE (n:Node {id:1})",
		"DROP INDEX ON :Node(name)",
	}
	for _, q := range writeQueries {
		_, err := b.Cypher(service.CypherRequest{Repo: "test", Query: q})
		if err == nil {
			t.Errorf("expected error for write query %q", q)
		}
	}
}

func TestCypher_LimitAndSkip(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	// LIMIT only
	result, err := b.Cypher(service.CypherRequest{Repo: "test", Query: "MATCH (n) RETURN n.name LIMIT 3"})
	if err != nil {
		t.Fatalf("Cypher LIMIT: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Errorf("expected 3 rows with LIMIT 3, got %d", len(result.Rows))
	}

	// SKIP only
	result, err = b.Cypher(service.CypherRequest{Repo: "test", Query: "MATCH (n) RETURN n.name SKIP 2"})
	if err != nil {
		t.Fatalf("Cypher SKIP: %v", err)
	}
	// Total nodes from buildTestGraph: 4 functions + 2 files + 1 process = 7.
	// After skipping 2, should have 5.
	total := 7
	expected := total - 2
	if len(result.Rows) != expected {
		t.Errorf("expected %d rows with SKIP 2, got %d", expected, len(result.Rows))
	}

	// SKIP + LIMIT
	result, err = b.Cypher(service.CypherRequest{Repo: "test", Query: "MATCH (n) RETURN n.name SKIP 1 LIMIT 2"})
	if err != nil {
		t.Fatalf("Cypher SKIP+LIMIT: %v", err)
	}
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows with SKIP 1 LIMIT 2, got %d", len(result.Rows))
	}
}

func TestStripLimitSkip(t *testing.T) {
	tests := []struct {
		input     string
		wantQuery string
		wantSkip  int
		wantLimit int
	}{
		{"MATCH (n) RETURN n LIMIT 5", "MATCH (n) RETURN n", -1, 5},
		{"MATCH (n) RETURN n SKIP 10", "MATCH (n) RETURN n", 10, -1},
		{"MATCH (n) RETURN n SKIP 2 LIMIT 10", "MATCH (n) RETURN n", 2, 10},
		{"MATCH (n) RETURN n", "MATCH (n) RETURN n", -1, -1},
		{"MATCH (n) RETURN n limit 100", "MATCH (n) RETURN n", -1, 100},
	}
	for _, tt := range tests {
		q, s, l := stripLimitSkip(tt.input)
		if q != tt.wantQuery {
			t.Errorf("stripLimitSkip(%q): query=%q, want %q", tt.input, q, tt.wantQuery)
		}
		if s != tt.wantSkip {
			t.Errorf("stripLimitSkip(%q): skip=%d, want %d", tt.input, s, tt.wantSkip)
		}
		if l != tt.wantLimit {
			t.Errorf("stripLimitSkip(%q): limit=%d, want %d", tt.input, l, tt.wantLimit)
		}
	}
}

func TestStripOrderBy(t *testing.T) {
	tests := []struct {
		input    string
		wantQ    string
		wantCols []string
		wantAsc  []bool
	}{
		{
			"MATCH (n) RETURN n.name ORDER BY n.name",
			"MATCH (n) RETURN n.name",
			[]string{"n.name"},
			[]bool{true},
		},
		{
			"MATCH (n) RETURN n.name, n.score ORDER BY n.score DESC",
			"MATCH (n) RETURN n.name, n.score",
			[]string{"n.score"},
			[]bool{false},
		},
		{
			"MATCH (n) RETURN n.name ORDER BY n.score DESC LIMIT 10",
			"MATCH (n) RETURN n.name LIMIT 10",
			[]string{"n.score"},
			[]bool{false},
		},
		{
			"MATCH (n) RETURN n.name ORDER BY n.a ASC, n.b DESC",
			"MATCH (n) RETURN n.name",
			[]string{"n.a", "n.b"},
			[]bool{true, false},
		},
		{
			"MATCH (n) RETURN n.name",
			"MATCH (n) RETURN n.name",
			nil, nil,
		},
	}
	for _, tt := range tests {
		q, cols, asc := stripOrderBy(tt.input)
		if q != tt.wantQ {
			t.Errorf("stripOrderBy(%q): query=%q, want %q", tt.input, q, tt.wantQ)
		}
		if len(cols) != len(tt.wantCols) {
			t.Errorf("stripOrderBy(%q): cols=%v, want %v", tt.input, cols, tt.wantCols)
			continue
		}
		for i := range cols {
			if cols[i] != tt.wantCols[i] {
				t.Errorf("stripOrderBy(%q): col[%d]=%q, want %q", tt.input, i, cols[i], tt.wantCols[i])
			}
			if asc[i] != tt.wantAsc[i] {
				t.Errorf("stripOrderBy(%q): asc[%d]=%v, want %v", tt.input, i, asc[i], tt.wantAsc[i])
			}
		}
	}
}

func TestCypher_OrderBy(t *testing.T) {
	g := lpg.NewGraph()
	for _, s := range []struct {
		name  string
		score float64
	}{
		{testNameCharlie, 3.0},
		{"alice", 1.0},
		{testNameBob, 2.0},
	} {
		graph.AddNode(g, graph.LabelFunction, map[string]any{
			graph.PropID:       "func:" + s.name,
			graph.PropName:     s.name,
			"score":            s.score,
			graph.PropFilePath: s.name + ".go",
		})
	}
	b := &Backend{Graph: g}

	// ORDER BY score DESC
	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (f:Function) RETURN f.name, f.score ORDER BY f.score DESC",
	})
	if err != nil {
		t.Fatalf("Cypher ORDER BY: %v", err)
	}
	if len(result.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(result.Rows))
	}
	names := make([]string, len(result.Rows))
	for i, row := range result.Rows {
		// opencypher uses sequential column names: "1"=f.name, "2"=f.score
		names[i], _ = row[result.Columns[0]].(string)
	}
	if names[0] != testNameCharlie || names[1] != testNameBob || names[2] != "alice" {
		t.Errorf("expected [charlie bob alice], got %v", names)
	}

	// ORDER BY score DESC LIMIT 2
	result2, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (f:Function) RETURN f.name, f.score ORDER BY f.score DESC LIMIT 2",
	})
	if err != nil {
		t.Fatalf("Cypher ORDER BY + LIMIT: %v", err)
	}
	if len(result2.Rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result2.Rows))
	}
	n0, _ := result2.Rows[0][result2.Columns[0]].(string)
	n1, _ := result2.Rows[1][result2.Columns[0]].(string)
	if n0 != testNameCharlie || n1 != testNameBob {
		t.Errorf("expected [charlie bob] with ORDER BY DESC LIMIT 2, got [%s %s]", n0, n1)
	}

	// ORDER BY name ASC
	result3, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (f:Function) RETURN f.name ORDER BY f.name ASC",
	})
	if err != nil {
		t.Fatalf("Cypher ORDER BY ASC: %v", err)
	}
	namesAsc := make([]string, len(result3.Rows))
	for i, row := range result3.Rows {
		namesAsc[i], _ = row[result3.Columns[0]].(string)
	}
	if namesAsc[0] != "alice" || namesAsc[1] != testNameBob || namesAsc[2] != testNameCharlie {
		t.Errorf("expected [alice bob charlie], got %v", namesAsc)
	}
}

func TestCypher_Distinct(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	// Without DISTINCT: should return one labels() value per node.
	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n) RETURN labels(n)",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	allRows := len(result.Rows)
	t.Logf("Without DISTINCT: %d rows", allRows)

	// With DISTINCT: should have fewer rows (deduplicated label sets).
	result2, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n) RETURN DISTINCT labels(n)",
	})
	if err != nil {
		t.Fatalf("Cypher DISTINCT: %v", err)
	}
	t.Logf("With DISTINCT: %d rows", len(result2.Rows))
	if len(result2.Rows) >= allRows {
		t.Errorf("DISTINCT should reduce rows: got %d (was %d)", len(result2.Rows), allRows)
	}
}

func TestCypher_CountStar(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n) RETURN count(*) AS total",
	})
	if err != nil {
		t.Fatalf("Cypher count(*): %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	total := result.Rows[0]["total"]
	if total == nil || total == 0 {
		t.Errorf("expected non-zero count, got %v", total)
	}
	t.Logf("count(*) = %v", total)
}

func TestCypher_CountStarNoLabel(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function) RETURN count(*)",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestCypher_TypeR(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (a)-[r]->(b) RETURN a.name, type(r) AS relType, b.name",
	})
	if err != nil {
		t.Fatalf("Cypher type(r): %v", err)
	}
	if len(result.Rows) == 0 {
		t.Error("expected rows for MATCH (a)-[r]->(b)")
	}
	found := false
	for _, row := range result.Rows {
		if rt, ok := row["relType"]; ok && rt != nil && rt != "" {
			found = true
			t.Logf("type(r) = %v", rt)
			break
		}
	}
	if !found {
		t.Error("expected type(r) to return edge labels")
	}
}

func TestRewriteCountStar(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"MATCH (n) RETURN count(*)", "MATCH (n) RETURN count(n)"},
		{"MATCH (x:Function) RETURN count(*) AS cnt", "MATCH (x:Function) RETURN count(x) AS cnt"},
		{"MATCH (n) RETURN count(n)", "MATCH (n) RETURN count(n)"}, // no change
		{"MATCH (a)-[r]->(b) RETURN count(*)", "MATCH (a)-[r]->(b) RETURN count(a)"},
		{"MATCH (n) RETURN COUNT(*)", "MATCH (n) RETURN count(n)"}, // case-insensitive
	}
	for _, tt := range tests {
		got := rewriteCountStar(tt.input)
		if got != tt.want {
			t.Errorf("rewriteCountStar(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripDistinct(t *testing.T) {
	tests := []struct {
		input        string
		wantQuery    string
		wantDistinct bool
	}{
		{"MATCH (n) RETURN DISTINCT n.name", "MATCH (n) RETURN n.name", true},
		{"MATCH (n) RETURN n.name", "MATCH (n) RETURN n.name", false},
		{"MATCH (n) RETURN distinct labels(n)", "MATCH (n) RETURN labels(n)", true},
	}
	for _, tt := range tests {
		q, d := stripDistinct(tt.input)
		if q != tt.wantQuery {
			t.Errorf("stripDistinct(%q): query=%q, want %q", tt.input, q, tt.wantQuery)
		}
		if d != tt.wantDistinct {
			t.Errorf("stripDistinct(%q): distinct=%v, want %v", tt.input, d, tt.wantDistinct)
		}
	}
}

func TestFindSymbol_CaseInsensitive(t *testing.T) {
	g := buildTestGraph()

	// Exact match should work.
	node := findSymbol(g, testFuncHandleRequest, "", "")
	if node == nil {
		t.Fatal("expected to find handleRequest")
	}

	// Case-insensitive should also work.
	node = findSymbol(g, "HandleRequest", "", "")
	if node == nil {
		t.Fatal("expected case-insensitive match for HandleRequest")
	}
}

func TestImpact_Downstream(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    testFuncHandleRequest,
		Direction: "downstream",
		Depth:     5,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result.Target.Name != testFuncHandleRequest {
		t.Errorf("expected target handleRequest, got %q", result.Target.Name)
	}
	found := false
	for _, a := range result.Affected {
		if a.Name == testFuncMain {
			found = true
		}
	}
	if !found {
		t.Error("expected main in downstream affected nodes")
	}
}

func TestImpact_Upstream(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    testFuncHandleRequest,
		Direction: "upstream",
		Depth:     5,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	found := false
	for _, a := range result.Affected {
		if a.Name == testFuncValidate {
			found = true
		}
	}
	if !found {
		t.Error("expected validate in upstream affected nodes")
	}
}

func TestImpact_DefaultDepth(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    testFuncMain,
		Direction: "downstream",
		Depth:     0,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result.Depth != 5 {
		t.Errorf("expected default depth 5, got %d", result.Depth)
	}
}

func TestImpact_NotFound(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	_, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "nonexistent",
		Direction: "downstream",
		Depth:     5,
	})
	if err == nil {
		t.Error("expected error for nonexistent target")
	}
}

func TestImpact_DepthLimits(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    testFuncValidate,
		Direction: "downstream",
		Depth:     1,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(result.Affected) > 1 {
		t.Errorf("expected at most 1 affected with depth=1, got %d", len(result.Affected))
	}
}

func TestNodeToSymbolMatch(t *testing.T) {
	g := lpg.NewGraph()
	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:test", Name: "test"},
		FilePath:      "test.go",
		StartLine:     5,
		EndLine:       15,
		Content:       "func test() {}",
	})

	sm := nodeToSymbolMatch(fn, true)
	if sm.Name != "test" {
		t.Errorf("name: %q", sm.Name)
	}
	if sm.FilePath != "test.go" {
		t.Errorf("filePath: %q", sm.FilePath)
	}
	if sm.StartLine != 5 {
		t.Errorf("startLine: %d", sm.StartLine)
	}
	if sm.Label != "Function" {
		t.Errorf("label: %q", sm.Label)
	}
	if sm.Content != "func test() {}" {
		t.Errorf("content: %q", sm.Content)
	}

	sm2 := nodeToSymbolMatch(fn, false)
	if sm2.Content != "" {
		t.Errorf("expected empty content, got %q", sm2.Content)
	}
}

func TestFindSymbol_ByUID(t *testing.T) {
	g := buildTestGraph()
	node := findSymbol(g, "anything", "", "func:validate")
	if node == nil {
		t.Fatal("expected to find node by UID")
	}
	if graph.GetStringProp(node, graph.PropName) != testFuncValidate {
		t.Error("expected validate")
	}
}

func TestFindSymbol_ByNameAndFile(t *testing.T) {
	g := buildTestGraph()
	node := findSymbol(g, testFuncMain, testFileMainGo, "")
	if node == nil {
		t.Fatal("expected to find node")
	}
	if graph.GetStringProp(node, graph.PropFilePath) != testFileMainGo {
		t.Error("expected main.go")
	}
}

func TestFindSymbol_NotFound(t *testing.T) {
	g := buildTestGraph()
	node := findSymbol(g, "nonexistent", "", "")
	if node != nil {
		t.Error("expected nil for nonexistent")
	}
}

func TestSearchByName_Limit(t *testing.T) {
	g := buildTestGraph()
	results := searchByName(g, "a", 2, false)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results with limit, got %d", len(results))
	}
}

func TestCypher_RelationshipTraversal(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (a)-[r]->(b) RETURN a.name, b.name",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if len(result.Rows) == 0 {
		t.Error("expected rows for relationship traversal")
	}
}

func TestCypher_TypedRelationshipTraversal(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	// MATCH with a typed relationship label (e.g., :CALLS) should work
	// now that edges use the relationship type as their label.
	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (a)-[:CALLS]->(b) RETURN a.name, b.name",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if len(result.Rows) == 0 {
		t.Error("expected rows for typed :CALLS relationship traversal")
	}
}

func TestCypher_CountAggregation(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function) RETURN count(n) AS cnt",
	})
	if err != nil {
		t.Fatalf("Cypher count: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 aggregated row, got %d", len(result.Rows))
	}
	cnt, ok := result.Rows[0]["cnt"]
	if !ok {
		t.Fatal("expected 'cnt' column in result")
	}
	if cnt != 4 {
		t.Errorf("expected count=4, got %v", cnt)
	}
}

func TestCypher_CountNoAlias(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function) RETURN count(n)",
	})
	if err != nil {
		t.Fatalf("Cypher count: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 aggregated row, got %d", len(result.Rows))
	}
}

func TestCypher_CollectAggregation(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function) RETURN collect(n.name) AS names",
	})
	if err != nil {
		t.Fatalf("Cypher collect: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 aggregated row, got %d", len(result.Rows))
	}
	names, ok := result.Rows[0]["names"]
	if !ok {
		t.Fatal("expected 'names' column")
	}
	arr, ok := names.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", names)
	}
	if len(arr) != 4 {
		t.Errorf("expected 4 collected names, got %d: %v", len(arr), arr)
	}
}

func TestCypher_ScalarFunctions(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	tests := []struct {
		name  string
		query string
	}{
		{"toLower", "MATCH (n:Function {name: 'main'}) RETURN toLower(n.name)"},
		{"toUpper", "MATCH (n:Function {name: 'main'}) RETURN toUpper(n.name)"},
		{"id", "MATCH (n:Function {name: 'main'}) RETURN id(n)"},
		{"coalesce", "MATCH (n:Function {name: 'main'}) RETURN coalesce(n.name, 'unknown')"},
		{"labels", "MATCH (n:Function {name: 'main'}) RETURN labels(n)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := b.Cypher(service.CypherRequest{
				Repo:  "test",
				Query: tc.query,
			})
			if err != nil {
				t.Fatalf("Cypher %s: %v", tc.name, err)
			}
			if len(result.Rows) == 0 {
				t.Errorf("expected rows for %s", tc.name)
			}
		})
	}
}

func TestCypher_EmptyResult(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function {name: 'nonexistent_xyz'}) RETURN n",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result.Rows))
	}
}

func TestCypher_ConsecutiveCallsWork(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	for i := range 3 {
		result, err := b.Cypher(service.CypherRequest{
			Repo:  "test",
			Query: "MATCH (n:Function) RETURN n.name",
		})
		if err != nil {
			t.Fatalf("Cypher call %d: %v", i, err)
		}
		if len(result.Rows) < 3 {
			t.Errorf("call %d: expected at least 3 rows, got %d", i, len(result.Rows))
		}
	}
}

func TestCypher_OptionalMatch(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "OPTIONAL MATCH (n:Function {name: 'nonexistent'}) RETURN n",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	_ = result
}

func TestCypher_ColumnsReturned(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: "MATCH (n:Function) RETURN n.name AS funcName, n.filePath AS path",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if len(result.Columns) < 2 {
		t.Errorf("expected at least 2 columns, got %d", len(result.Columns))
	}
}

func TestImpact_ViaImportEdges(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "func:main",
		Direction: "downstream",
		Depth:     5,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	_ = result
}

func TestImpact_ZeroDepthUsesDefault(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    testFuncValidate,
		Direction: "upstream",
		Depth:     0,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result.Depth != 5 {
		t.Errorf("expected default depth 5, got %d", result.Depth)
	}
}

func TestImpact_ByNodeID(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "func:validate",
		Direction: "downstream",
		Depth:     3,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result.Target.Name != testFuncValidate {
		t.Errorf("expected target validate, got %q", result.Target.Name)
	}
}

// buildInterfaceGraph creates a graph with an interface, implementors, and methods.
func buildInterfaceGraph() *lpg.Graph {
	g := lpg.NewGraph()

	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:GraphStore", Name: "GraphStore"},
		FilePath:      "store.go",
	})
	impl := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:BboltStore", Name: "BboltStore"},
		FilePath:      "bbolt/bbolt_store.go",
	})
	method := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:SaveGraph", Name: "SaveGraph"},
		FilePath:      "bbolt/bbolt_store.go",
	})
	caller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:runLocal", Name: "runLocal"},
		FilePath:      "cmd/root.go",
	})

	graph.AddEdge(g, impl, iface, graph.RelImplements, nil)
	graph.AddEdge(g, impl, method, graph.RelHasMethod, nil)
	graph.AddEdge(g, caller, method, graph.RelCalls, nil)

	return g
}

func TestImpact_DownstreamViaImplements(t *testing.T) {
	b := &Backend{Graph: buildInterfaceGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "GraphStore",
		Direction: "downstream",
		Depth:     3,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	found := false
	for _, a := range result.Affected {
		if a.Name == "BboltStore" {
			found = true
		}
	}
	if !found {
		names := make([]string, len(result.Affected))
		for i, a := range result.Affected {
			names[i] = a.Name
		}
		t.Errorf("expected BboltStore in affected, got %v", names)
	}
}

func TestImpact_UpstreamViaExtends(t *testing.T) {
	g := lpg.NewGraph()
	base := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Base", Name: "Base"},
		FilePath:      "base.go",
	})
	child := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Child", Name: "Child"},
		FilePath:      "child.go",
	})
	graph.AddEdge(g, child, base, graph.RelExtends, nil)

	b := &Backend{Graph: g}
	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "Child",
		Direction: "upstream",
		Depth:     3,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	found := false
	for _, a := range result.Affected {
		if a.Name == "Base" {
			found = true
		}
	}
	if !found {
		t.Error("expected Base in upstream impact of Child")
	}
}

func TestImpact_FileDisambiguation(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Pipeline.Run", Name: "Run"},
		FilePath:      "pipeline.go",
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Server.Run", Name: "Run"},
		FilePath:      "server.go",
	})

	b := &Backend{Graph: g}
	result, err := b.Impact(service.ImpactRequest{
		Repo:   "test",
		Target: "Run",
		File:   "pipeline.go",
		Depth:  1,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result.Target.FilePath != "pipeline.go" {
		t.Errorf("expected target in pipeline.go, got %q", result.Target.FilePath)
	}
}

func TestContext_DeduplicatesCallers(t *testing.T) {
	g := lpg.NewGraph()
	caller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:a", Name: "a"},
		FilePath:      "a.go",
	})
	callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:b", Name: "b"},
		FilePath:      "b.go",
	})
	graph.AddEdge(g, caller, callee, graph.RelCalls, nil)
	graph.AddEdge(g, caller, callee, graph.RelCalls, nil)

	b := &Backend{Graph: g}
	result, err := b.Context(service.ContextRequest{Name: "b", Content: false})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if len(result.Callers) != 1 {
		t.Errorf("expected 1 deduplicated caller, got %d", len(result.Callers))
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"handler_test.go", true},
		{"handler.go", false},
		{"src/handler.test.ts", true},
		{"src/handler.spec.js", true},
		{"src/handler.ts", false},
		{"test_handler.py", true},
		{"handler.py", false},
		{"__tests__/handler.js", true},
		{"testdata/fixture.go", true},
		{"internal/query/backend.go", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := isTestFile(tc.path); got != tc.want {
				t.Errorf("isTestFile(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// --- Additional Query tests ---

func TestQuery_IncludeContent(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: testFuncValidate, Limit: 5, Content: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Definitions) == 0 {
		t.Fatal("expected at least 1 definition")
	}
	found := false
	for _, d := range result.Definitions {
		if d.Name == testFuncValidate && d.Content != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected content to be included in query result for validate")
	}
}

func TestQuery_ExcludeContent(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: testFuncValidate, Limit: 5, Content: false})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for _, d := range result.Definitions {
		if d.Content != "" {
			t.Errorf("expected empty content, got %q", d.Content)
		}
	}
}

// --- Additional Context tests ---

func TestContext_ImportRelationships(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	// server.go imports main.go — look at file-level context.
	// The context tool is symbol-level, but we can test with the functions.
	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncMain})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	// main has callees (handleRequest).
	if len(result.Callees) == 0 {
		t.Error("expected at least 1 callee for main")
	}
}

func TestContext_FileSuffix(t *testing.T) {
	b := &Backend{Graph: buildTestGraph()}

	// Find by file suffix.
	result, err := b.Context(service.ContextRequest{Repo: "test", Name: testFuncMain, File: testFileMainGo})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result.Symbol.FilePath != testFileMainGo {
		t.Errorf("expected main.go, got %q", result.Symbol.FilePath)
	}
}

// --- Helper edge case tests ---

func TestFindSymbolByNameOrID_ByID(t *testing.T) {
	g := buildTestGraph()
	node := findSymbolByNameOrID(g, "func:validate", "")
	if node == nil {
		t.Fatal("expected to find node by ID")
	}
	if graph.GetStringProp(node, graph.PropName) != testFuncValidate {
		t.Error("expected validate")
	}
}

func TestFindSymbolByNameOrID_ByName(t *testing.T) {
	g := buildTestGraph()
	node := findSymbolByNameOrID(g, testFuncValidate, "")
	if node == nil {
		t.Fatal("expected to find node by name")
	}
}

func TestFindSymbolByNameOrID_NotFound(t *testing.T) {
	g := buildTestGraph()
	node := findSymbolByNameOrID(g, "nonexistent_zzz", "")
	if node != nil {
		t.Error("expected nil for nonexistent")
	}
}

// TestFindSymbol_FileNoMatch verifies that an explicit file filter that
// doesn't match any candidate returns nil instead of falling through to
// an unrelated symbol (regression fix for issue #6).
func TestFindSymbol_FileNoMatch(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:Run:root", Name: "Run"},
		FilePath:      "cmd/root.go",
	})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "method:Run:pipeline", Name: "Run"},
		FilePath:      "internal/ingestion/pipeline.go",
	})

	// With correct file, should find the pipeline method.
	node := findSymbol(g, "Run", "internal/ingestion/pipeline.go", "")
	if node == nil {
		t.Fatal("expected to find Run in pipeline.go")
	}
	if graph.GetStringProp(node, graph.PropFilePath) != "internal/ingestion/pipeline.go" {
		t.Errorf("expected pipeline.go, got %q", graph.GetStringProp(node, graph.PropFilePath))
	}

	// With a file that doesn't match any candidate, must return nil,
	// NOT fall through to the wrong "Run".
	node = findSymbol(g, "Run", "nonexistent/file.go", "")
	if node != nil {
		t.Errorf("expected nil when file doesn't match, got %q in %q",
			graph.GetStringProp(node, graph.PropName),
			graph.GetStringProp(node, graph.PropFilePath))
	}
}

// TestImpact_DownstreamViaUsesEdge verifies that downstream impact finds
// functions referencing an interface type via USES edges (Go structural typing).
func TestImpact_DownstreamViaUsesEdge(t *testing.T) {
	g := lpg.NewGraph()

	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Store", Name: "Store"},
		FilePath:      "store.go",
	})
	consumer := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:initPipeline", Name: "initPipeline"},
		FilePath:      "pipeline.go",
	})
	// initPipeline references the Store interface as a parameter type.
	graph.AddEdge(g, consumer, iface, graph.RelUses, nil)

	b := &Backend{Graph: g}
	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "Store",
		Direction: "downstream",
		Depth:     3,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}

	names := make(map[string]bool)
	for _, a := range result.Affected {
		names[a.Name] = true
	}
	if !names["initPipeline"] {
		t.Errorf("expected initPipeline in downstream affected via USES edge, got %v", names)
	}
}

// TestImpact_UpstreamViaUsesEdge verifies that upstream impact from a function
// discovers the types that function references via USES edges.
func TestImpact_UpstreamViaUsesEdge(t *testing.T) {
	g := lpg.NewGraph()

	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Logger", Name: "Logger"},
		FilePath:      "logger.go",
	})
	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:serve", Name: "serve"},
		FilePath:      "server.go",
	})
	// serve references Logger.
	graph.AddEdge(g, fn, iface, graph.RelUses, nil)

	b := &Backend{Graph: g}
	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "serve",
		Direction: "upstream",
		Depth:     3,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}

	names := make(map[string]bool)
	for _, a := range result.Affected {
		names[a.Name] = true
	}
	if !names["Logger"] {
		t.Errorf("expected Logger in upstream affected via USES edge, got %v", names)
	}
}

// TestImpact_DownstreamFollowsMethodsOfImplementor verifies that downstream
// impact from an interface traces through implementor structs to their
// methods and then to callers of those methods.
func TestImpact_DownstreamFollowsMethodsOfImplementor(t *testing.T) {
	b := &Backend{Graph: buildInterfaceGraph()}

	result, err := b.Impact(service.ImpactRequest{
		Repo:      "test",
		Target:    "GraphStore",
		Direction: "downstream",
		Depth:     5,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	names := make(map[string]bool)
	for _, a := range result.Affected {
		names[a.Name] = true
	}
	// BboltStore implements GraphStore → should be found.
	if !names["BboltStore"] {
		t.Error("expected BboltStore in affected")
	}
	// SaveGraph is a method of BboltStore → should be found via HAS_METHOD.
	if !names["SaveGraph"] {
		t.Errorf("expected SaveGraph in affected (via outgoing HAS_METHOD), got %v", names)
	}
	// runLocal calls SaveGraph → should be found via CALLS.
	if !names["runLocal"] {
		t.Errorf("expected runLocal in affected (calls SaveGraph), got %v", names)
	}
}

func TestCypherValueToAny_PrimitiveValues(t *testing.T) {
	// Test with plain values.
	result := cypherValueToAny(opencypher.RValue{Value: "hello"})
	if result != "hello" {
		t.Errorf("expected 'hello', got %v", result)
	}

	result2 := cypherValueToAny(opencypher.RValue{Value: int64(42)})
	if result2 != int64(42) {
		t.Errorf("expected 42, got %v", result2)
	}
}

func TestCypherValueToAny_Node(t *testing.T) {
	g := lpg.NewGraph()
	n := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:test", Name: "test"},
		FilePath:      "test.go",
	})

	result := cypherValueToAny(opencypher.RValue{Value: n})
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m[graph.PropName] != "test" {
		t.Errorf("expected name 'test', got %v", m[graph.PropName])
	}
	if m["_labels"] == nil {
		t.Error("expected _labels in node map")
	}
}

func TestCypherValueToAny_ValueList(t *testing.T) {
	vals := []opencypher.Value{
		opencypher.RValue{Value: "a"},
		opencypher.RValue{Value: "b"},
	}
	result := cypherValueToAny(opencypher.RValue{Value: vals})
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", result)
	}
	if len(arr) != 2 {
		t.Errorf("expected 2 elements, got %d", len(arr))
	}
}

// --- Score propagation tests ---

func TestQuery_WithIndex_ScoresPropagated(t *testing.T) {
	g := buildTestGraph()
	b := &Backend{Graph: g}

	// Build a search index and wire it up.
	ix, err := search.NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	b.Index = ix

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: testFuncHandleRequest, Limit: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(result.Definitions) == 0 {
		t.Fatal("expected at least 1 definition")
	}

	// The top result should have a non-zero score (from BM25/RRF).
	top := result.Definitions[0]
	if top.Score <= 0 {
		t.Errorf("expected positive score on top result, got %.6f", top.Score)
	}
	t.Logf("top result: %s score=%.6f", top.Name, top.Score)
}

func TestQuery_WithIndex_ProcessRelevanceWeighted(t *testing.T) {
	g := buildTestGraph()
	b := &Backend{Graph: g}

	ix, err := search.NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	b.Index = ix

	// Search for "handleRequest" — it's step 2 of main-flow.
	result, err := b.Query(service.QueryRequest{Repo: "test", Text: testFuncHandleRequest, Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(result.Processes) == 0 {
		t.Fatal("expected at least 1 process match")
	}

	// Process relevance should be > 0 (weighted by search score, not just 1.0).
	for _, p := range result.Processes {
		if p.Relevance <= 0 {
			t.Errorf("process %q has zero relevance", p.Name)
		}
		t.Logf("process: %s relevance=%.4f", p.Name, p.Relevance)
	}
}

func TestQuery_WithIndex_MultiFieldFusion(t *testing.T) {
	g := buildTestGraph()
	b := &Backend{Graph: g}

	ix, err := search.NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()

	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	b.Index = ix

	// "token" only appears in content of validate's body: "func validate(token string) error { }".
	// With multi-field search, it should still find validate via content match.
	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "token", Limit: 5})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	found := false
	for _, d := range result.Definitions {
		if d.Name == testFuncValidate {
			found = true
			if d.Score <= 0 {
				t.Errorf("expected positive score for validate, got %.6f", d.Score)
			}
		}
	}
	if !found {
		t.Error("expected validate in results for 'token' (content-field match)")
	}
}

// --- Deduplication tests ---

func TestDeduplicateDefinitions_NamePlusFilePath(t *testing.T) {
	defs := []service.SymbolMatch{
		{Name: "Allocations", FilePath: "state_store.go", Score: 0.9},
		{Name: "Allocations", FilePath: "client.go", Score: 0.8},
		{Name: "Allocations", FilePath: "state_store.go", Score: 0.7}, // true duplicate
		{Name: "StateStore", FilePath: "state_store.go", Score: 0.6},
	}
	result := deduplicateDefinitions(defs)

	if len(result) != 3 {
		t.Fatalf("expected 3 entries after dedup, got %d", len(result))
	}
	// The duplicate (state_store.go + Allocations, score 0.7) should be gone.
	for _, d := range result {
		if d.Name == "Allocations" && d.FilePath == "state_store.go" && d.Score == 0.7 {
			t.Error("expected lower-scored duplicate to be removed")
		}
	}
}

func TestCapPerName(t *testing.T) {
	defs := []service.SymbolMatch{
		{Name: "Allocations", FilePath: "a.go", Score: 0.9},
		{Name: "Allocations", FilePath: "b.go", Score: 0.8},
		{Name: "Allocations", FilePath: "c.go", Score: 0.7},
		{Name: "Allocations", FilePath: "d.go", Score: 0.6},
		{Name: "Allocations", FilePath: "e.go", Score: 0.5},
		{Name: "StateStore", FilePath: "store.go", Score: 0.4},
	}
	result := capPerName(defs)

	allocCount := 0
	for _, d := range result {
		if d.Name == "Allocations" {
			allocCount++
		}
	}
	if allocCount != 3 {
		t.Errorf("expected 3 Allocations entries, got %d", allocCount)
	}
	if len(result) != 4 { // 3 Allocations + 1 StateStore
		t.Errorf("expected 4 total entries, got %d", len(result))
	}
}

func TestCapPerName_BelowCap(t *testing.T) {
	defs := []service.SymbolMatch{
		{Name: "Foo", FilePath: "a.go", Score: 0.9},
		{Name: "Foo", FilePath: "b.go", Score: 0.8},
		{Name: "Bar", FilePath: "c.go", Score: 0.7},
	}
	result := capPerName(defs)
	if len(result) != 3 {
		t.Errorf("expected 3 entries (all below cap), got %d", len(result))
	}
}

func TestQuery_CrossListDedup(t *testing.T) {
	// Build a graph where a symbol appears in definitions AND in a process.
	// After cross-list dedup, processSymbols should not repeat it.
	g := lpg.NewGraph()

	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:handler", Name: "handler"},
		FilePath:      "handler.go",
		StartLine:     1,
		EndLine:       10,
		Content:       "func handler() {}",
	})

	proc := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps:  graph.BaseNodeProps{ID: "process:flow", Name: "handler-flow"},
		EntryPoint:     "func:handler",
		HeuristicLabel: "Handler",
		StepCount:      1,
	})
	graph.AddTypedEdge(g, fn, proc, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 1})

	b := &Backend{Graph: g}
	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "handler", Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// handler should appear in definitions.
	if len(result.Definitions) == 0 {
		t.Fatal("expected handler in definitions")
	}

	// After cross-list dedup, handler should NOT appear in processSymbols
	// since it's already in definitions.
	for _, ps := range result.ProcessSymbols {
		if ps.Name == "handler" && ps.FilePath == "handler.go" {
			t.Error("handler should be removed from processSymbols (cross-list dedup)")
		}
	}
}

func TestQuery_UsageExamplesPartition(t *testing.T) {
	// Build a graph with both a normal function and a test function.
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:eval", Name: "Eval"},
		FilePath:      "eval.go",
		StartLine:     1,
		EndLine:       10,
		Content:       "func Eval() {}",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:test_eval", Name: "TestEval"},
		FilePath:      "eval_test.go",
		StartLine:     1,
		EndLine:       10,
		Content:       "func TestEval(t *testing.T) {}",
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:example_eval", Name: "ExampleEval"},
		FilePath:      "examples/eval_example.go",
		StartLine:     1,
		EndLine:       10,
		Content:       "func ExampleEval() {}",
	})

	b := &Backend{Graph: g}
	// IncludeTests=true to verify partition into UsageExamples.
	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "eval", Limit: 10, IncludeTests: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// Eval should be in definitions (not a test/example file).
	foundInDefs := false
	for _, d := range result.Definitions {
		if d.Name == "Eval" && d.FilePath == "eval.go" {
			foundInDefs = true
		}
		if d.FilePath == "eval_test.go" || d.FilePath == "examples/eval_example.go" {
			t.Errorf("usage file %q should not be in definitions", d.FilePath)
		}
	}
	if !foundInDefs {
		t.Error("expected Eval in definitions")
	}

	// TestEval and ExampleEval should be in usageExamples.
	if len(result.UsageExamples) != 2 {
		t.Errorf("expected 2 usage examples, got %d", len(result.UsageExamples))
	}
}

func TestQuery_IncludeTestsFlag(t *testing.T) {
	g := lpg.NewGraph()

	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:eval", Name: "Eval"},
		FilePath:      "eval.go",
		StartLine:     1,
		EndLine:       10,
	})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:test_eval", Name: "TestEval"},
		FilePath:      "eval_test.go",
		StartLine:     1,
		EndLine:       10,
	})

	b := &Backend{Graph: g}

	// Default (IncludeTests=false): test files excluded entirely.
	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "eval", Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.UsageExamples) != 0 {
		t.Errorf("expected 0 usage examples by default, got %d", len(result.UsageExamples))
	}
	for _, d := range result.Definitions {
		if d.FilePath == "eval_test.go" {
			t.Error("test file should not be in definitions by default")
		}
	}

	// With IncludeTests=true: test files appear in UsageExamples.
	result2, err := b.Query(service.QueryRequest{Repo: "test", Text: "eval", Limit: 10, IncludeTests: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result2.UsageExamples) == 0 {
		t.Error("expected usage examples with IncludeTests=true")
	}
}

func TestQuery_TestFlowPartition(t *testing.T) {
	g := lpg.NewGraph()

	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:handler", Name: "handler"},
		FilePath:      "handler.go",
		StartLine:     1,
		EndLine:       10,
		Content:       "func handler() {}",
	})

	// Non-test process.
	proc := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps:  graph.BaseNodeProps{ID: "process:handler-flow", Name: "handler-flow"},
		EntryPoint:     "func:handler",
		HeuristicLabel: "Request handler",
		StepCount:      1,
	})
	graph.AddTypedEdge(g, fn, proc, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 1})

	// Test process.
	testFn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:testHandler", Name: "testHandler"},
		FilePath:      "handler.go",
		StartLine:     20,
		EndLine:       30,
		Content:       "func testHandler() {}",
	})
	testProc := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps:  graph.BaseNodeProps{ID: "process:testHandler-flow", Name: "testHandler-flow"},
		EntryPoint:     "func:testHandler",
		HeuristicLabel: "Test flow",
		StepCount:      1,
	})
	graph.AddTypedEdge(g, testFn, testProc, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 1})

	b := &Backend{Graph: g}
	// IncludeTests=true to verify test flows are partitioned into TestFlows.
	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "handler", Limit: 10, IncludeTests: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// handler-flow should be in processes.
	foundArch := false
	for _, p := range result.Processes {
		if p.Name == "handler-flow" {
			foundArch = true
		}
		if p.HeuristicLabel == "Test flow" {
			t.Error("test flow should not be in main processes")
		}
	}
	if !foundArch {
		t.Error("expected handler-flow in main processes")
	}

	// testHandler-flow should be in testFlows.
	foundTest := false
	for _, tf := range result.TestFlows {
		if tf.Name == "testHandler-flow" {
			foundTest = true
		}
	}
	if !foundTest {
		t.Error("expected testHandler-flow in testFlows")
	}
}

func TestContext_Implementors(t *testing.T) {
	g := lpg.NewGraph()

	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "iface:Scheduler", Name: "Scheduler"},
		FilePath:      "scheduler.go",
		StartLine:     1,
		EndLine:       10,
	})
	impl1 := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:GenericScheduler", Name: "GenericScheduler"},
		FilePath:      "generic_scheduler.go",
		StartLine:     1,
		EndLine:       50,
	})
	impl2 := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "struct:SystemScheduler", Name: "SystemScheduler"},
		FilePath:      "system_scheduler.go",
		StartLine:     1,
		EndLine:       30,
	})
	graph.AddEdge(g, impl1, iface, graph.RelImplements, nil)
	graph.AddEdge(g, impl2, iface, graph.RelImplements, nil)

	b := &Backend{Graph: g}
	result, err := b.Context(service.ContextRequest{Repo: "test", Name: "Scheduler"})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}

	if len(result.Implementors) != 2 {
		t.Fatalf("expected 2 implementors, got %d", len(result.Implementors))
	}

	implNames := map[string]bool{}
	for _, impl := range result.Implementors {
		implNames[impl.Name] = true
	}
	if !implNames["GenericScheduler"] {
		t.Error("expected GenericScheduler in implementors")
	}
	if !implNames["SystemScheduler"] {
		t.Error("expected SystemScheduler in implementors")
	}
}

func TestContext_Extends(t *testing.T) {
	g := lpg.NewGraph()

	parent := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Base", Name: "Base"},
		FilePath:      "base.go",
		StartLine:     1,
		EndLine:       10,
	})
	child := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "class:Derived", Name: "Derived"},
		FilePath:      "derived.go",
		StartLine:     1,
		EndLine:       20,
	})
	graph.AddEdge(g, child, parent, graph.RelExtends, nil)

	b := &Backend{Graph: g}
	result, err := b.Context(service.ContextRequest{Repo: "test", Name: "Derived"})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}

	if len(result.Extends) != 1 {
		t.Fatalf("expected 1 extends, got %d", len(result.Extends))
	}
	if result.Extends[0].Name != "Base" {
		t.Errorf("expected extends Base, got %q", result.Extends[0].Name)
	}
}

func TestCypher_NegativePatternIncoming(t *testing.T) {
	// Test NOT ()-[:CALLS]->(f): find functions that are never called.
	g := lpg.NewGraph()

	// fnA is called by fnB, fnC is not called by anyone.
	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnA", Name: "fnA"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       5,
		IsExported:    true,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnB", Name: "fnB"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       5,
		IsExported:    true,
	})
	fnC := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnC", Name: "fnC"},
		FilePath:      "c.go",
		StartLine:     1,
		EndLine:       5,
		IsExported:    true,
	})
	// fnB calls fnA.
	graph.AddEdge(g, fnB, fnA, graph.RelCalls, nil)
	_ = fnC

	b := &Backend{Graph: g}
	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: `MATCH (f:Function) WHERE NOT ()-[:CALLS]->(f) RETURN f.name LIMIT 15`,
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}

	// fnA is called by fnB, so it should NOT appear.
	// fnB and fnC are not called, so they should appear.
	names := make(map[string]bool)
	for _, row := range result.Rows {
		// opencypher uses numeric column keys ("1" for first RETURN expr).
		for _, v := range row {
			if name, ok := v.(string); ok {
				names[name] = true
			}
		}
	}

	if names["fnA"] {
		t.Error("fnA is called by fnB, should not appear in results")
	}
	if !names["fnB"] {
		t.Error("fnB is not called by anyone, should appear in results")
	}
	if !names["fnC"] {
		t.Error("fnC is not called by anyone, should appear in results")
	}
}

func TestCypher_NegativePatternOutgoing(t *testing.T) {
	// Test NOT (f)-[:CALLS]->(): find functions that don't call anything.
	g := lpg.NewGraph()

	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnA", Name: "fnA"},
		FilePath:      "a.go",
		StartLine:     1,
		EndLine:       5,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnB", Name: "fnB"},
		FilePath:      "b.go",
		StartLine:     1,
		EndLine:       5,
	})
	// fnA calls fnB.
	graph.AddEdge(g, fnA, fnB, graph.RelCalls, nil)

	b := &Backend{Graph: g}
	result, err := b.Cypher(service.CypherRequest{
		Repo:  "test",
		Query: `MATCH (f:Function) WHERE NOT (f)-[:CALLS]->() RETURN f.name`,
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}

	// fnA calls fnB, so it should NOT appear.
	// fnB doesn't call anyone, so it should appear.
	names := make(map[string]bool)
	for _, row := range result.Rows {
		// opencypher uses numeric column keys ("1" for first RETURN expr).
		for _, v := range row {
			if name, ok := v.(string); ok {
				names[name] = true
			}
		}
	}

	if names["fnA"] {
		t.Error("fnA calls fnB, should not appear in results")
	}
	if !names["fnB"] {
		t.Error("fnB doesn't call anything, should appear in results")
	}
}

func TestRewriteNegativePattern(t *testing.T) {
	g := lpg.NewGraph()

	// Create two functions; fnB calls fnA.
	fnA := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnA", Name: "fnA"},
		FilePath:      "a.go", StartLine: 1, EndLine: 5,
	})
	fnB := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:fnB", Name: "fnB"},
		FilePath:      "b.go", StartLine: 1, EndLine: 5,
	})
	graph.AddEdge(g, fnB, fnA, graph.RelCalls, nil)
	b := &Backend{Graph: g}

	tests := []struct {
		input         string
		expectChanged bool // whether the query is modified
	}{
		{
			input:         `MATCH (f:Function) WHERE NOT ()-[:CALLS]->(f) RETURN f.name`,
			expectChanged: true,
		},
		{
			input:         `MATCH (f:Function) WHERE NOT (f)-[:CALLS]->() RETURN f.name`,
			expectChanged: true,
		},
		{
			input:         `MATCH (f:Function) WHERE f.name = "test" RETURN f.name`,
			expectChanged: false,
		},
	}

	for _, tt := range tests {
		rewritten := b.rewriteNegativePattern(tt.input)
		changed := rewritten != tt.input
		if changed != tt.expectChanged {
			t.Errorf("rewriteNegativePattern(%q): expectChanged=%v, got changed=%v\n  rewritten: %q",
				tt.input, tt.expectChanged, changed, rewritten)
		}
		// Verify the NOT clause is gone.
		if tt.expectChanged && strings.Contains(strings.ToUpper(rewritten), "NOT (") {
			t.Errorf("rewritten query still contains NOT pattern: %q", rewritten)
		}
	}
}

func TestQuery_ProcessResultsCapped(t *testing.T) {
	// Build a graph with many processes that all share a step node matching
	// a query term, so they all appear in results.
	g := lpg.NewGraph()

	// Shared target function that matches query "worker".
	fnWorker := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "func:worker", Name: "runWorker"},
		FilePath:      "worker.go",
		StartLine:     1,
		EndLine:       10,
		Content:       "func runWorker() { }",
	})

	// Create 20 processes that each have fnWorker as a step.
	for i := range 20 {
		name := fmt.Sprintf("entry%d", i)
		pName := fmt.Sprintf("entry%d-flow", i)
		pID := "process:" + pName

		fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
			BaseNodeProps: graph.BaseNodeProps{ID: "func:" + name, Name: name},
			FilePath:      fmt.Sprintf("pkg%d/%s.go", i, name),
			StartLine:     1,
			EndLine:       10,
		})
		pNode := graph.AddProcessNode(g, graph.ProcessProps{
			BaseNodeProps:  graph.BaseNodeProps{ID: pID, Name: pName},
			EntryPoint:     "func:" + name,
			HeuristicLabel: "Processing",
			StepCount:      2,
			CallerCount:    i, // varying centrality
		})

		graph.AddEdge(g, fn, fnWorker, graph.RelCalls, nil)
		graph.AddTypedEdge(g, fn, pNode, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 1})
		graph.AddTypedEdge(g, fnWorker, pNode, graph.EdgeProps{Type: graph.RelStepInProcess, Step: 2})
	}

	ix, err := search.NewMemoryIndex()
	if err != nil {
		t.Fatalf("NewMemoryIndex: %v", err)
	}
	defer ix.Close()
	if _, err := ix.IndexGraph(g); err != nil {
		t.Fatalf("IndexGraph: %v", err)
	}
	b := &Backend{Graph: g, Index: ix}

	result, err := b.Query(service.QueryRequest{Repo: "test", Text: "runWorker", Limit: 20})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// Should be capped at maxProcessResults (10), not all 20.
	if len(result.Processes) > 10 {
		t.Errorf("expected ≤10 process results (capped), got %d", len(result.Processes))
	}

	// All returned processes should have relevance ≥ minProcessRelevance (0.05).
	for _, p := range result.Processes {
		if p.Relevance < 0.05 && len(result.Processes) > 1 {
			t.Errorf("process %q has relevance %.4f below minimum threshold", p.Name, p.Relevance)
		}
	}
}
