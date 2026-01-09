package embedding

import (
	"strings"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

// quickGraph builds a small graph with a file, function, class, and
// method wired with edges for testing text generation.
func quickGraph(t *testing.T) (*lpg.Graph, *lpg.Node, *lpg.Node, *lpg.Node, *lpg.Node) {
	t.Helper()
	g := lpg.NewGraph()

	file := graph.AddFileNode(g, graph.FileProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "f1", Name: "handler.go"},
		FilePath:      "internal/server/handler.go",
		Language:      "go",
	})

	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn1", Name: "HandleRequest"},
		FilePath:      "internal/server/handler.go",
		StartLine:     10,
		EndLine:       30,
		Content:       "func HandleRequest(w http.ResponseWriter, r *http.Request) {\n\ttoken := validateToken(r)\n\treturn\n}",
		Description:   "HandleRequest processes incoming HTTP requests, validates the auth token, and dispatches to the appropriate handler.",
		Signature:     "func HandleRequest(w http.ResponseWriter, r *http.Request)",
		IsExported:    true,
	})

	class := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "c1", Name: "Server"},
		FilePath:      "internal/server/server.go",
		StartLine:     5,
		EndLine:       50,
		Content:       "type Server struct {\n\trouter *Router\n}",
		Description:   "Server is the main HTTP server.",
	})

	method := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "m1", Name: "Start"},
		FilePath:      "internal/server/server.go",
		StartLine:     52,
		EndLine:       65,
		Content:       "func (s *Server) Start() error {\n\treturn http.ListenAndServe(s.addr, s.router)\n}",
		Signature:     "func (s *Server) Start() error",
	})

	callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn2", Name: "validateToken"},
		FilePath:      "internal/auth/token.go",
	})

	caller := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn3", Name: "main"},
		FilePath:      "cmd/main.go",
	})

	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "i1", Name: "Handler"},
		FilePath:      "internal/server/handler.go",
	})

	graph.AddEdge(g, file, fn, graph.RelContains, nil)
	graph.AddEdge(g, file, class, graph.RelContains, nil)
	graph.AddEdge(g, fn, callee, graph.RelCalls, nil)       // HandleRequest → validateToken
	graph.AddEdge(g, caller, fn, graph.RelCalls, nil)       // main → HandleRequest
	graph.AddEdge(g, method, class, graph.RelMemberOf, nil) // Start → Server
	graph.AddEdge(g, class, method, graph.RelHasMethod, nil)
	graph.AddEdge(g, class, iface, graph.RelImplements, nil) // Server implements Handler

	return g, file, fn, class, method
}

func TestGenerateEmbeddingText_Function(t *testing.T) {
	g, _, fn, _, _ := quickGraph(t)

	text := GenerateEmbeddingText(fn, g)

	for _, want := range []string{
		"Function: HandleRequest",
		"File: internal/server/handler.go",
		"Calls: validateToken",
		"Called by: main",
		"Doc: HandleRequest processes",
		"func HandleRequest",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in output, got:\n%s", want, text)
		}
	}
}

func TestGenerateEmbeddingText_Method(t *testing.T) {
	g, _, _, _, method := quickGraph(t)

	text := GenerateEmbeddingText(method, g)

	for _, want := range []string{
		"Method: Start",
		"Member of: Server",
		"func (s *Server) Start() error",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in output, got:\n%s", want, text)
		}
	}
}

func TestGenerateEmbeddingText_Class(t *testing.T) {
	g, _, _, class, _ := quickGraph(t)

	text := GenerateEmbeddingText(class, g)

	for _, want := range []string{
		"Class: Server",
		"Implements: Handler",
		"Methods: Start",
		"Doc: Server is the main HTTP server.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in output, got:\n%s", want, text)
		}
	}
}

func TestGenerateEmbeddingText_File(t *testing.T) {
	g, file, _, _, _ := quickGraph(t)

	text := GenerateEmbeddingText(file, g)

	for _, want := range []string{
		"File: internal/server/handler.go",
		"Language: go",
		"Defines:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in output, got:\n%s", want, text)
		}
	}

	if !strings.Contains(text, "HandleRequest") {
		t.Errorf("expected 'HandleRequest' in file text, got:\n%s", text)
	}
}

func TestGenerateEmbeddingText_NoGraph(t *testing.T) {
	g := lpg.NewGraph()
	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn1", Name: "doWork"},
		FilePath:      "worker.go",
		Content:       "func doWork() {}",
	})

	// Pass nil graph — should still produce text without graph context.
	text := GenerateEmbeddingText(fn, nil)

	if !strings.Contains(text, "Function: doWork") {
		t.Errorf("expected 'Function: doWork', got:\n%s", text)
	}
	if !strings.Contains(text, "func doWork()") {
		t.Errorf("expected code snippet, got:\n%s", text)
	}
	// Should NOT contain graph context lines.
	if strings.Contains(text, "Calls:") || strings.Contains(text, "Called by:") {
		t.Errorf("should not have graph context when g is nil, got:\n%s", text)
	}
}

func TestGenerateBatchTexts(t *testing.T) {
	g, _, fn, class, _ := quickGraph(t)
	nodes := []*lpg.Node{fn, class}

	texts := GenerateBatchTexts(nodes, g)

	if len(texts) != 2 {
		t.Fatalf("expected 2 texts, got %d", len(texts))
	}
	if !strings.Contains(texts[0], "HandleRequest") {
		t.Errorf("text[0] should contain 'HandleRequest', got:\n%s", texts[0])
	}
	if !strings.Contains(texts[1], "Server") {
		t.Errorf("text[1] should contain 'Server', got:\n%s", texts[1])
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	// With nil tokenizer, 1 token ≈ 4 chars.
	// 100 tokens = ~400 chars.
	long := strings.Repeat("word ", 200) // 1000 chars
	truncated := truncateToTokenBudget(long, 100)

	if len(truncated) >= len(long) {
		t.Errorf("expected truncation, got len=%d (original=%d)", len(truncated), len(long))
	}
	// Should be roughly 400 chars (100 tokens × 4 chars/token).
	if len(truncated) < 300 || len(truncated) > 500 {
		t.Errorf("expected ~400 chars, got %d", len(truncated))
	}
}

func TestTruncateToTokenBudget_Short(t *testing.T) {
	short := "hello world"
	result := truncateToTokenBudget(short, 100)
	if result != short {
		t.Errorf("short text should not be truncated, got %q", result)
	}
}

func TestPackageFromPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"internal/server/handler.go", "internal/server"},
		{"src/pkg/utils/helpers.go", "pkg/utils"},
		{"main.go", ""},
		{"a/b.go", "a"},
	}
	for _, tt := range tests {
		got := packageFromPath(tt.input)
		if got != tt.want {
			t.Errorf("packageFromPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestJoinMax(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	if got := joinMax(items, 3); got != "a, b, c" {
		t.Errorf("joinMax(5, 3) = %q", got)
	}
	if got := joinMax(items, 10); got != "a, b, c, d, e" {
		t.Errorf("joinMax(5, 10) = %q", got)
	}
}

func TestCountTokens_NilTokenizer(t *testing.T) {
	// Nil tokenizer uses 1 token per 4 chars.
	if got := countTokens("hello world!"); got != 3 {
		t.Errorf("countTokens(12 chars) = %d, want 3", got)
	}
	if got := countTokens(""); got != 0 {
		t.Errorf("countTokens('') = %d, want 0", got)
	}
}

func TestShouldEmbed(t *testing.T) {
	g := lpg.NewGraph()

	// Exported function with doc comment — should embed.
	exported := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn1", Name: "HandleRequest"},
		FilePath:      "server.go",
		IsExported:    true,
		Description:   "Handles HTTP requests.",
	})
	if !ShouldEmbed(exported, g) {
		t.Error("exported + doc comment: want true")
	}

	// Unexported function, no doc, no connections — should NOT embed.
	private := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn2", Name: "parseHeader"},
		FilePath:      "server.go",
		IsExported:    false,
	})
	if ShouldEmbed(private, g) {
		t.Error("private, no doc, no connections: want false")
	}

	// Unexported function WITH doc comment — should embed.
	privateDocs := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn3", Name: "reconcile"},
		FilePath:      "reconciler.go",
		IsExported:    false,
		Description:   "Reconciles desired state with actual state.",
	})
	if !ShouldEmbed(privateDocs, g) {
		t.Error("private with doc comment: want true")
	}

	// Exported function without doc — should embed (public API).
	exportedNoDocs := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn4", Name: "Close"},
		FilePath:      "server.go",
		IsExported:    true,
	})
	if !ShouldEmbed(exportedNoDocs, g) {
		t.Error("exported without doc: want true")
	}

	// Class — always embed.
	class := graph.AddSymbolNode(g, graph.LabelClass, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "c1", Name: "Server"},
		FilePath:      "server.go",
	})
	if !ShouldEmbed(class, g) {
		t.Error("class: want true (always embed)")
	}

	// Interface — always embed.
	iface := graph.AddSymbolNode(g, graph.LabelInterface, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "i1", Name: "Handler"},
		FilePath:      "handler.go",
	})
	if !ShouldEmbed(iface, g) {
		t.Error("interface: want true (always embed)")
	}

	// Struct — always embed.
	strct := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "s1", Name: "Config"},
		FilePath:      "config.go",
	})
	if !ShouldEmbed(strct, g) {
		t.Error("struct: want true (always embed)")
	}

	// Unexported function with high connectivity (3+ edges) — should embed.
	hub := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn5", Name: "dispatch"},
		FilePath:      "dispatch.go",
		IsExported:    false,
	})
	caller1 := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn6", Name: "a"},
		FilePath:      "a.go",
	})
	caller2 := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn7", Name: "b"},
		FilePath:      "b.go",
	})
	caller3 := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn8", Name: "c"},
		FilePath:      "c.go",
	})
	graph.AddEdge(g, caller1, hub, graph.RelCalls, nil)
	graph.AddEdge(g, caller2, hub, graph.RelCalls, nil)
	graph.AddEdge(g, caller3, hub, graph.RelCalls, nil)
	if !ShouldEmbed(hub, g) {
		t.Error("private with 3 callers: want true")
	}

	// ShouldEmbed with nil graph — skips centrality check.
	if ShouldEmbed(private, nil) {
		t.Error("private, no doc, nil graph: want false")
	}

	// Test-file nodes are always excluded.
	testFunc := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "tf1", Name: "TestServerStartup"},
		FilePath:      "server_test.go",
		IsExported:    true,
		Description:   "Tests server startup.",
	})
	testFunc.SetProperty(graph.PropIsTest, true)
	if ShouldEmbed(testFunc, g) {
		t.Error("test file function: want false")
	}

	// Test exclusion takes priority over structural type.
	testStruct := graph.AddSymbolNode(g, graph.LabelStruct, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "ts1", Name: "MockServer"},
		FilePath:      "server_test.go",
	})
	testStruct.SetProperty(graph.PropIsTest, true)
	if ShouldEmbed(testStruct, g) {
		t.Error("struct in test file: want false")
	}

	// Trivial exported getter (≤3 lines, no doc) — skip.
	getter := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "triv1", Name: "Region"},
		FilePath:      "server.go",
		IsExported:    true,
		StartLine:     10,
		EndLine:       12,
	})
	if ShouldEmbed(getter, g) {
		t.Error("trivial getter (≤3 lines, no doc): want false")
	}

	// Doc overrides trivial-skip.
	trivialWithDoc := graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "triv2", Name: "Name"},
		FilePath:      "server.go",
		IsExported:    true,
		StartLine:     20,
		EndLine:       22,
		Description:   "Name returns the server's name.",
	})
	if !ShouldEmbed(trivialWithDoc, g) {
		t.Error("trivial with doc: want true (doc overrides)")
	}

	// Non-trivial exported (10 lines, no doc) — embed.
	nonTrivial := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "triv3", Name: "Setup"},
		FilePath:      "server.go",
		IsExported:    true,
		StartLine:     30,
		EndLine:       40,
	})
	if !ShouldEmbed(nonTrivial, g) {
		t.Error("non-trivial exported (10 lines): want true")
	}
}

func TestProseSummary(t *testing.T) {
	g := lpg.NewGraph()

	fn := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn1", Name: "HandleRequest"},
		FilePath:      "internal/server/handler.go",
		StartLine:     10, EndLine: 30,
		Content:    "func HandleRequest(w http.ResponseWriter, r *http.Request) {}",
		Signature:  "func HandleRequest(w http.ResponseWriter, r *http.Request)",
		IsExported: true,
	})

	// Cross-package callee
	callee := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn2", Name: "validateToken"},
		FilePath:      "internal/auth/token.go",
	})
	graph.AddEdge(g, fn, callee, graph.RelCalls, nil)

	// Spawned function
	spawned := graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{
		BaseNodeProps: graph.BaseNodeProps{ID: "fn3", Name: "refreshAsync"},
		FilePath:      "internal/auth/refresh.go",
	})
	graph.AddEdge(g, fn, spawned, graph.RelSpawns, nil)

	// Process membership
	proc := graph.AddProcessNode(g, graph.ProcessProps{
		BaseNodeProps:  graph.BaseNodeProps{ID: "p1", Name: "request-handler-flow"},
		HeuristicLabel: "Request handler",
		Importance:     150,
	})
	graph.AddEdge(g, fn, proc, graph.RelStepInProcess, nil)

	text := GenerateEmbeddingText(fn, g)
	t.Logf("Generated text:\n%s", text)

	for _, want := range []string{
		"Summary:",
		"Request handler",
		"bridges internal/auth",
		"spawns concurrent refreshAsync",
		"part of the request-handler-flow flow",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in summary, got:\n%s", want, text)
		}
	}
}
