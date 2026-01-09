package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/graph"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
)

// newTestMemoryClient creates a MemoryClient with a stub backend for
// "testrepo". The graph is in-memory — no disk or server involved.
func newTestMemoryClient(t *testing.T) *MemoryClient {
	t.Helper()
	mc := NewMemoryClient("")

	g := lpg.NewGraph()
	g.NewNode([]string{"File"}, map[string]any{
		graph.PropID:   "file://main.go",
		graph.PropName: "main.go",
		graph.PropType: "file",
	})
	g.NewNode([]string{"Function"}, map[string]any{
		graph.PropID:   "func://main",
		graph.PropName: "main",
		graph.PropType: "function",
	})

	idx, err := search.NewMemoryIndex()
	if err != nil {
		t.Fatalf("create memory index: %v", err)
	}
	if _, err := idx.IndexGraph(g); err != nil {
		t.Fatalf("index graph: %v", err)
	}

	mc.LoadGraph("testrepo", g, idx)

	mc.SetBackendFactory(func(repo string) ToolBackend {
		rg, ri, ok := mc.GetRepoResources(repo)
		if !ok {
			return nil
		}
		_ = rg
		_ = ri
		return stubBackend{}
	})

	t.Cleanup(mc.Close)
	return mc
}

func TestMemoryClient_Query(t *testing.T) {
	mc := newTestMemoryClient(t)
	res, err := mc.Query(QueryRequest{Repo: "testrepo", Text: "main", Limit: 5})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestMemoryClient_Context(t *testing.T) {
	mc := newTestMemoryClient(t)
	res, err := mc.Context(ContextRequest{Repo: "testrepo", Name: "main"})
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestMemoryClient_Cypher(t *testing.T) {
	mc := newTestMemoryClient(t)
	res, err := mc.Cypher(CypherRequest{Repo: "testrepo", Query: "MATCH (n) RETURN n"})
	if err != nil {
		t.Fatalf("cypher: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestMemoryClient_Impact(t *testing.T) {
	mc := newTestMemoryClient(t)
	res, err := mc.Impact(ImpactRequest{Repo: "testrepo", Target: "main", Depth: 3})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestMemoryClient_Status(t *testing.T) {
	mc := newTestMemoryClient(t)
	res, err := mc.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !res.Running {
		t.Error("expected Running=true")
	}
	if !res.Ready {
		t.Error("expected Ready=true (repo is loaded)")
	}
	if len(res.LoadedRepos) != 1 {
		t.Errorf("expected 1 loaded repo, got %d", len(res.LoadedRepos))
	}
}

func TestMemoryClient_Shutdown(t *testing.T) {
	mc := newTestMemoryClient(t)
	if err := mc.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestMemoryClient_MissingRepo(t *testing.T) {
	mc := NewMemoryClient("")
	mc.SetBackendFactory(func(repo string) ToolBackend { return nil })

	_, err := mc.Query(QueryRequest{Repo: "nonexistent", Text: "test"})
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestMemoryClient_EmptyRepo(t *testing.T) {
	mc := NewMemoryClient("")
	_, err := mc.Query(QueryRequest{Repo: "", Text: "test"})
	if err == nil {
		t.Fatal("expected error for empty repo")
	}
}

func TestMemoryClient_Source_NoResolver(t *testing.T) {
	mc := newTestMemoryClient(t)
	_, err := mc.Source(SourceRequest{Repo: "testrepo", Files: []string{"main.go"}})
	if err == nil {
		t.Fatal("expected error when no content resolver is set")
	}
}

func TestMemoryClient_Source_WithDiskResolver(t *testing.T) {
	mc := newTestMemoryClient(t)

	tmpDir := t.TempDir()
	testFile := "hello.txt"
	content := "hello world\nline 2\nline 3\n"
	if err := writeTestFile(tmpDir, testFile, content); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	mc.SetContentResolver("testrepo", &storage.ContentResolver{
		SourcePath: tmpDir,
	})

	res, err := mc.Source(SourceRequest{Repo: "testrepo", Files: []string{testFile}})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	if res.Files[0].Content != content {
		t.Errorf("content mismatch: got %q", res.Files[0].Content)
	}
	if res.Files[0].LineCount != 3 {
		t.Errorf("expected 3 lines, got %d", res.Files[0].LineCount)
	}
}

func TestMemoryClient_Source_LineRange(t *testing.T) {
	mc := newTestMemoryClient(t)

	tmpDir := t.TempDir()
	testFile := "multi.txt"
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := writeTestFile(tmpDir, testFile, content); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	mc.SetContentResolver("testrepo", &storage.ContentResolver{
		SourcePath: tmpDir,
	})

	res, err := mc.Source(SourceRequest{
		Repo:  "testrepo",
		Files: []string{testFile},
		Lines: "2-4",
	})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	want := "line2\nline3\nline4"
	if res.Files[0].Content != want {
		t.Errorf("line range content: got %q, want %q", res.Files[0].Content, want)
	}
}

func TestMemoryClient_Reload_NoDataDir(t *testing.T) {
	mc := NewMemoryClient("")
	err := mc.Reload(ReloadRequest{Repo: "testrepo"})
	if err == nil {
		t.Fatal("expected error when reloading with no data dir")
	}
}

func TestMemoryClient_StatusEmpty(t *testing.T) {
	mc := NewMemoryClient("")
	res, err := mc.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if res.Ready {
		t.Error("expected Ready=false for empty client")
	}
	if len(res.LoadedRepos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(res.LoadedRepos))
	}
}

func TestMemoryClient_GetRepoResources(t *testing.T) {
	mc := newTestMemoryClient(t)

	g, idx, ok := mc.GetRepoResources("testrepo")
	if !ok {
		t.Fatal("expected testrepo to be loaded")
	}
	if g == nil {
		t.Error("expected non-nil graph")
	}
	if idx == nil {
		t.Error("expected non-nil index")
	}

	_, _, ok = mc.GetRepoResources("nope")
	if ok {
		t.Error("expected false for nonexistent repo")
	}
}

func TestMemoryClient_ResolveRepoName_InMemoryFastPath(t *testing.T) {
	mc := newTestMemoryClient(t)

	got, err := mc.resolveRepoName("testrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "testrepo" {
		t.Errorf("got %q, want %q", got, "testrepo")
	}
}

func TestMemoryClient_ResolveRepoName_ShortNameViaRegistry(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "hashicorp/consul", Hash: "h1"})

	mc := NewMemoryClient(dir)
	t.Cleanup(mc.Close)

	got, err := mc.resolveRepoName("consul")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hashicorp/consul" {
		t.Errorf("got %q, want %q", got, "hashicorp/consul")
	}
}

func TestMemoryClient_ResolveRepoName_AmbiguousShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "acme/lib", Hash: "h1"})
	_ = reg.Add(storage.RegistryEntry{Name: "corp/lib", Hash: "h2"})

	mc := NewMemoryClient(dir)
	t.Cleanup(mc.Close)

	_, err := mc.resolveRepoName("lib")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got: %v", err)
	}
}

func TestMemoryClient_ResolveRepoName_NoDataDir(t *testing.T) {
	mc := NewMemoryClient("")
	t.Cleanup(mc.Close)

	got, err := mc.resolveRepoName("anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "anything" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestMemoryClient_QueryAmbiguousShortName(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "acme/sdk", Hash: "h1"})
	_ = reg.Add(storage.RegistryEntry{Name: "corp/sdk", Hash: "h2"})

	mc := NewMemoryClient(dir)
	mc.SetBackendFactory(func(repo string) ToolBackend { return stubBackend{} })
	t.Cleanup(mc.Close)

	_, err := mc.Query(QueryRequest{Repo: "sdk", Text: "test"})
	if err == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got: %v", err)
	}
}

// writeTestFile creates a file in dir with the given content.
func writeTestFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}
