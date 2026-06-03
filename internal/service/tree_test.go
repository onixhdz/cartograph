package service

import (
	"strings"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/graph"
)

func TestBuildTreeResultIncludesFileSymbols(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{BaseNodeProps: graph.BaseNodeProps{ID: "file:a", Name: "a.go"}, FilePath: "./a.go"})
	graph.AddFileNode(g, graph.FileProps{BaseNodeProps: graph.BaseNodeProps{ID: "file:b", Name: "b.go"}, FilePath: "b.go"})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:first", Name: "first"}, FilePath: "a.go", StartLine: 1, EndLine: 3})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:z", Name: "zeta"}, FilePath: "a.go", StartLine: 20, EndLine: 24, Signature: "func zeta()"})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:a", Name: "alpha"}, FilePath: "./a.go", StartLine: 10, EndLine: 12})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:b", Name: "beta"}, FilePath: "a.go", StartLine: 10, EndLine: 13})

	result := BuildTreeResult("repo", g)

	wantFiles := "a.go,b.go"
	if got := strings.Join(result.Files, ","); got != wantFiles {
		t.Fatalf("files = %q, want %q", got, wantFiles)
	}

	symbols := result.FileSymbols["a.go"]
	if len(symbols) != 4 {
		t.Fatalf("a.go symbols = %d, want 4", len(symbols))
	}
	if got := symbols[0].Name; got != "first" {
		t.Fatalf("first symbol = %q, want first", got)
	}
	if symbols[0].StartLine != 1 || symbols[0].EndLine != 3 {
		t.Fatalf("first symbol lines = %d-%d, want 1-3", symbols[0].StartLine, symbols[0].EndLine)
	}
	if symbols[1].UID != "fn:a" || symbols[1].StartLine != 10 || symbols[1].EndLine != 12 || symbols[1].Label != string(graph.LabelMethod) || symbols[1].Repo != "repo" {
		t.Fatalf("unexpected symbol payload: %#v", symbols[1])
	}
	if symbols[3].Signature != "func zeta()" {
		t.Fatalf("signature = %q, want func zeta()", symbols[3].Signature)
	}
}

func TestBuildTreeResultSkipsInvalidSymbols(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{BaseNodeProps: graph.BaseNodeProps{ID: "file:a", Name: "a.go"}, FilePath: "a.go"})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:bad-start", Name: "badStart"}, FilePath: "a.go", StartLine: -1, EndLine: 10})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:no-file", Name: "missingFile"}, StartLine: 1, EndLine: 10})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:no-name"}, FilePath: "a.go", StartLine: 1, EndLine: 10})

	result := BuildTreeResult("repo", g)

	if len(result.FileSymbols) != 0 {
		t.Fatalf("fileSymbols = %#v, want empty", result.FileSymbols)
	}
}
