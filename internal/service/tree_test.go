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
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:z", Name: "zeta"}, FilePath: "a.go", StartLine: 20, EndLine: 24, Signature: "func zeta()"})
	graph.AddSymbolNode(g, graph.LabelMethod, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:a", Name: "alpha"}, FilePath: "./a.go", StartLine: 10, EndLine: 12})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:b", Name: "beta"}, FilePath: "a.go", StartLine: 10, EndLine: 13})

	result := BuildTreeResult("repo", g)

	wantFiles := "a.go,b.go"
	if got := strings.Join(result.Files, ","); got != wantFiles {
		t.Fatalf("files = %q, want %q", got, wantFiles)
	}

	symbols := result.FileSymbols["a.go"]
	if len(symbols) != 3 {
		t.Fatalf("a.go symbols = %d, want 3", len(symbols))
	}
	if got := symbols[0].Name; got != "alpha" {
		t.Fatalf("first symbol = %q, want alpha", got)
	}
	if symbols[0].UID != "fn:a" || symbols[0].StartLine != 10 || symbols[0].EndLine != 12 || symbols[0].Label != string(graph.LabelMethod) || symbols[0].Repo != "repo" {
		t.Fatalf("unexpected symbol payload: %#v", symbols[0])
	}
	if symbols[2].Signature != "func zeta()" {
		t.Fatalf("signature = %q, want func zeta()", symbols[2].Signature)
	}
}

func TestBuildTreeResultSkipsInvalidSymbols(t *testing.T) {
	g := lpg.NewGraph()
	graph.AddFileNode(g, graph.FileProps{BaseNodeProps: graph.BaseNodeProps{ID: "file:a", Name: "a.go"}, FilePath: "a.go"})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:no-start", Name: "missingStart"}, FilePath: "a.go", StartLine: 0, EndLine: 10})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:no-file", Name: "missingFile"}, StartLine: 1, EndLine: 10})
	graph.AddSymbolNode(g, graph.LabelFunction, graph.SymbolProps{BaseNodeProps: graph.BaseNodeProps{ID: "fn:no-name"}, FilePath: "a.go", StartLine: 1, EndLine: 10})

	result := BuildTreeResult("repo", g)

	if len(result.FileSymbols) != 0 {
		t.Fatalf("fileSymbols = %#v, want empty", result.FileSymbols)
	}
}
