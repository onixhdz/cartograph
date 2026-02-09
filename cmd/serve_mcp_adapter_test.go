package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
	"github.com/realxen/cartograph/internal/testutil"
)

// newTestServerMCPClient creates a serverMCPClient backed by a real
// service.Server with testutil.SampleGraph loaded under "testrepo".
// It also sets up a ContentResolver using sourceDir as the on-disk
// source root. The server is started and cleaned up automatically.
func newTestServerMCPClient(t *testing.T, sourceDir string) *serverMCPClient {
	t.Helper()

	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "cartograph")
	_ = os.MkdirAll(dataDir, 0o750)

	socketPath := filepath.Join(dataDir, "test-adapter.sock")
	lf := service.NewLockfile(dataDir)
	t.Cleanup(func() { _ = lf.Release() })

	if err := lf.Acquire(socketPath, "unix"); err != nil {
		t.Fatalf("acquire lockfile: %v", err)
	}

	srv, err := service.NewServer(socketPath, lf, dataDir)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.SetBackendFactory(newServerBackendFactory(srv))

	g := testutil.SampleGraph()
	idx, err := search.NewMemoryIndex()
	if err != nil {
		t.Fatalf("new search index: %v", err)
	}
	if _, err := idx.IndexGraph(g); err != nil {
		t.Fatalf("index graph: %v", err)
	}
	srv.LoadGraphDirect("testrepo", g, idx)

	if sourceDir != "" {
		srv.SetContentResolver("testrepo", &storage.ContentResolver{
			SourcePath: sourceDir,
		})
	}

	srv.SetIdleTimeout(0) // disable idle shutdown in tests
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	return &serverMCPClient{srv: srv}
}

func TestServerMCPClient_Query(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	result, err := client.Query(service.QueryRequest{
		Repo:  "testrepo",
		Text:  "main",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil query result")
	}
	// The sample graph has a main function and main-flow process —
	// we should get at least one match.
	total := len(result.Processes) + len(result.Definitions) + len(result.ProcessSymbols)
	if total == 0 {
		t.Error("expected at least one result for 'main' query")
	}
}

func TestServerMCPClient_Context(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	result, err := client.Context(service.ContextRequest{
		Repo: "testrepo",
		Name: "main",
	})
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil context result")
	}
	if result.Symbol.Name == "" {
		t.Error("expected non-empty symbol name in context result")
	}
}

func TestServerMCPClient_Schema(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	result, err := client.Schema(service.SchemaRequest{
		Repo: "testrepo",
	})
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil schema result")
	}
	if result.TotalNodes == 0 {
		t.Error("expected non-zero total nodes in schema")
	}
	if result.TotalEdges == 0 {
		t.Error("expected non-zero total edges in schema")
	}
}

func TestServerMCPClient_Impact(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	result, err := client.Impact(service.ImpactRequest{
		Repo:      "testrepo",
		Target:    "main",
		Direction: "downstream",
		Depth:     2,
	})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil impact result")
	}
}

func TestServerMCPClient_Cypher(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	result, err := client.Cypher(service.CypherRequest{
		Repo:  "testrepo",
		Query: "MATCH (n) RETURN count(n) AS cnt",
	})
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil cypher result")
	}
	if len(result.Rows) == 0 {
		t.Error("expected at least one row from cypher query")
	}
}

func TestServerMCPClient_Cat(t *testing.T) {
	// Create a temp source directory with a test file.
	sourceDir := testutil.TempDir(t, map[string]string{
		"src/main.go": "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	})

	client := newTestServerMCPClient(t, sourceDir)

	result, err := client.Cat(service.CatRequest{
		Repo:  "testrepo",
		Files: []string{"src/main.go"},
	})
	if err != nil {
		t.Fatalf("Cat: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	f := result.Files[0]
	if f.Error != "" {
		t.Fatalf("unexpected error for src/main.go: %s", f.Error)
	}
	if !strings.Contains(f.Content, "package main") {
		t.Error("expected 'package main' in file content")
	}
	if f.LineCount != 5 {
		t.Errorf("expected 5 lines, got %d", f.LineCount)
	}
}

func TestServerMCPClient_CatLineRange(t *testing.T) {
	sourceDir := testutil.TempDir(t, map[string]string{
		"src/main.go": "line1\nline2\nline3\nline4\nline5\n",
	})

	client := newTestServerMCPClient(t, sourceDir)

	result, err := client.Cat(service.CatRequest{
		Repo:  "testrepo",
		Files: []string{"src/main.go"},
		Lines: "2-4",
	})
	if err != nil {
		t.Fatalf("Cat with lines: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(result.Files))
	}
	content := result.Files[0].Content
	if !strings.Contains(content, "line2") || !strings.Contains(content, "line4") {
		t.Errorf("expected lines 2-4, got: %q", content)
	}
	if strings.Contains(content, "line1") || strings.Contains(content, "line5") {
		t.Errorf("should not contain line1 or line5, got: %q", content)
	}
}

func TestServerMCPClient_CatInvalidLineRange(t *testing.T) {
	sourceDir := testutil.TempDir(t, map[string]string{
		"src/main.go": "line1\nline2\n",
	})

	client := newTestServerMCPClient(t, sourceDir)

	_, err := client.Cat(service.CatRequest{
		Repo:  "testrepo",
		Files: []string{"src/main.go"},
		Lines: "bad",
	})
	if err == nil {
		t.Fatal("expected error for invalid line range")
	}
	if !strings.Contains(err.Error(), "line range") {
		t.Errorf("expected 'line range' in error, got: %v", err)
	}
}

func TestServerMCPClient_CatMissingFile(t *testing.T) {
	sourceDir := testutil.TempDir(t, map[string]string{
		"src/main.go": "package main\n",
	})

	client := newTestServerMCPClient(t, sourceDir)

	result, err := client.Cat(service.CatRequest{
		Repo:  "testrepo",
		Files: []string{"nonexistent.go"},
	})
	if err != nil {
		t.Fatalf("Cat should not return error for missing file: %v", err)
	}
	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(result.Files))
	}
	if result.Files[0].Error == "" {
		t.Error("expected error string for missing file")
	}
}

func TestServerMCPClient_Status(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	result, err := client.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil status result")
	}
	if len(result.LoadedRepos) != 1 {
		t.Fatalf("expected 1 loaded repo, got %d", len(result.LoadedRepos))
	}
	if result.LoadedRepos[0].Name != "testrepo" {
		t.Errorf("expected repo name 'testrepo', got %q", result.LoadedRepos[0].Name)
	}
	if result.LoadedRepos[0].NodeCount == 0 {
		t.Error("expected non-zero node count")
	}
}

func TestServerMCPClient_RepoNotFound(t *testing.T) {
	client := newTestServerMCPClient(t, "")

	_, err := client.Query(service.QueryRequest{
		Repo: "nonexistent",
		Text: "test",
	})
	if err == nil {
		t.Fatal("expected error for unknown repo")
	}
}

func TestServerMCPClient_CatNoResolver(t *testing.T) {
	// Create a client without a content resolver to test
	// the error path when no resolver is available.
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "cartograph")
	_ = os.MkdirAll(dataDir, 0o750)

	socketPath := filepath.Join(dataDir, "test-adapter-nores.sock")
	lf := service.NewLockfile(dataDir)
	t.Cleanup(func() { _ = lf.Release() })

	if err := lf.Acquire(socketPath, "unix"); err != nil {
		t.Fatalf("acquire lockfile: %v", err)
	}

	srv, err := service.NewServer(socketPath, lf, dataDir)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.SetBackendFactory(newServerBackendFactory(srv))

	g := testutil.SampleGraph()
	srv.LoadGraphDirect("testrepo", g, nil)
	srv.SetIdleTimeout(0)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	client := &serverMCPClient{srv: srv}

	_, err = client.Cat(service.CatRequest{
		Repo:  "testrepo",
		Files: []string{"src/main.go"},
	})
	if err == nil {
		t.Fatal("expected error when no content resolver is available")
	}
	if !strings.Contains(err.Error(), "no content resolver") {
		t.Errorf("expected 'no content resolver' in error, got: %v", err)
	}
}
