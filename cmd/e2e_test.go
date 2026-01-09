package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realxen/cartograph/internal/query"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
)

// TestE2E_AnalyzeURL_QuerySource exercises analyze→query→source end-to-end.
// Requires network access; skipped in short mode.
func TestE2E_AnalyzeURL_QuerySource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E network test in short mode")
	}

	// ── Setup: isolated data directory + real service ──────────────

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	dataDir := filepath.Join(tmpDir, "cartograph")
	os.MkdirAll(dataDir, 0o755) //nolint:errcheck

	socketPath := filepath.Join(dataDir, "test-e2e.sock")
	lf := service.NewLockfile(dataDir)
	t.Cleanup(func() { lf.Release() }) //nolint:errcheck

	if err := lf.Acquire(socketPath, "unix"); err != nil {
		t.Fatalf("acquire lockfile: %v", err)
	}

	srv, err := service.NewServer(socketPath, lf, dataDir)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.SetBackendFactory(func(repo string) service.ToolBackend {
		g, idx, ok := srv.GetRepoResources(repo)
		if !ok {
			return nil
		}
		return &query.Backend{Graph: g, Index: idx}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { srv.Stop() }) //nolint:errcheck

	client := service.NewAutoClient(srv.Addr)
	cli := &CLI{Client: client}

	// ── Step 1: Analyze a small public repo via URL ────────────────
	// go-billy is ~70 Go files, small and fast to clone.

	t.Log("Step 1: analyzing https://github.com/go-git/go-billy (in-memory)...")

	analyzeCmd := &AnalyzeCmd{
		Targets: []string{"https://github.com/go-git/go-billy"},
		Depth:   1,
		Embed:   "off",
	}
	out := captureStdout(t, func() {
		if err := analyzeCmd.Run(cli); err != nil {
			t.Fatalf("analyze: %v", err)
		}
	})
	t.Log(out)

	if !strings.Contains(out, "Graph:") {
		t.Errorf("expected 'Graph:' in analyze output, got:\n%s", out)
	}
	if !strings.Contains(out, "Done in") {
		t.Errorf("expected 'Done in' in analyze output, got:\n%s", out)
	}

	// ── Verify: registry entry ────────────────────────────────────

	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	entry, ok := registry.Get("go-git/go-billy")
	if !ok {
		t.Fatal("expected 'go-git/go-billy' in registry")
	}
	if entry.URL == "" {
		t.Error("expected non-empty URL in registry entry")
	}
	if entry.NodeCount == 0 {
		t.Error("expected non-zero node count in registry")
	}
	t.Logf("Registry: name=%s url=%s nodes=%d edges=%d", entry.Name, entry.URL, entry.NodeCount, entry.EdgeCount)

	if entry.Meta.CommitHash == "" {
		t.Error("expected non-empty commit hash in meta")
	}
	if !entry.Meta.HasContentBucket {
		t.Error("expected HasContentBucket=true for in-memory analyzed repo")
	}
	if entry.Meta.SourcePath != "" {
		t.Errorf("expected empty SourcePath for in-memory repo, got %q", entry.Meta.SourcePath)
	}
	t.Logf("Meta: commit=%s contentBucket=%v url=%s branch=%s",
		entry.Meta.CommitHash[:12], entry.Meta.HasContentBucket, entry.URL, entry.Meta.Branch)

	// ── Step 2: Query the graph ────────────────────────────────────

	t.Log("Step 2: querying for 'Filesystem'...")

	queryResult, err := client.Query(service.QueryRequest{
		Repo:  "go-git/go-billy",
		Text:  "Filesystem",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	totalResults := len(queryResult.Processes) + len(queryResult.Definitions)
	if totalResults == 0 {
		t.Error("expected at least one query result for 'Filesystem'")
	}
	t.Logf("Query results: %d processes, %d definitions",
		len(queryResult.Processes), len(queryResult.Definitions))
	for _, d := range queryResult.Definitions {
		t.Logf("  %s (%s) %s:%d", d.Name, d.Label, d.FilePath, d.StartLine)
	}

	// ── Step 3: Retrieve source code from the content bucket ───────

	t.Log("Step 3: retrieving source for fs.go...")

	sourceResult, err := client.Source(service.SourceRequest{
		Repo:  "go-git/go-billy",
		Files: []string{"fs.go"},
	})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(sourceResult.Files) == 0 {
		t.Fatal("expected at least one file in source result")
	}
	f := sourceResult.Files[0]
	if f.Error != "" {
		t.Fatalf("source error for fs.go: %s", f.Error)
	}
	if f.LineCount == 0 {
		t.Error("expected non-zero line count for fs.go")
	}
	if !strings.Contains(f.Content, "package billy") {
		t.Error("expected 'package billy' in fs.go content")
	}
	t.Logf("Source: fs.go = %d lines", f.LineCount)

	// ── Step 4: Idempotency — re-analyze should skip ───────────────

	t.Log("Step 4: re-analyzing (should skip — same commit)...")

	out2 := captureStdout(t, func() {
		if err := analyzeCmd.Run(cli); err != nil {
			t.Fatalf("re-analyze: %v", err)
		}
	})
	if !strings.Contains(out2, "up to date") {
		t.Errorf("expected 'up to date' on re-analyze, got:\n%s", out2)
	}
	t.Log(out2)

	// ── Step 5: List command shows the repo ─────────────────────────

	t.Log("Step 5: list command...")

	listCmd := &ListCmd{}
	out3 := captureStdout(t, func() {
		if err := listCmd.Run(cli); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	if !strings.Contains(out3, "go-git/go-billy") {
		t.Errorf("expected 'go-git/go-billy' in list output, got:\n%s", out3)
	}
	if !strings.Contains(out3, "url") {
		t.Errorf("expected 'url' type in list output, got:\n%s", out3)
	}
	t.Log(out3)

	t.Log("✓ E2E test passed: analyze → query → source → idempotency → list")
}

// TestE2E_AnalyzeURL_CloneToDisk exercises the --clone mode.
func TestE2E_AnalyzeURL_CloneToDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E network test in short mode")
	}

	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	dataDir := filepath.Join(tmpDir, "cartograph")
	os.MkdirAll(dataDir, 0o755) //nolint:errcheck

	socketPath := filepath.Join(dataDir, "test-e2e-clone.sock")
	lf := service.NewLockfile(dataDir)
	t.Cleanup(func() { lf.Release() }) //nolint:errcheck

	if err := lf.Acquire(socketPath, "unix"); err != nil {
		t.Fatalf("acquire lockfile: %v", err)
	}

	srv, err := service.NewServer(socketPath, lf, dataDir)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.SetBackendFactory(func(repo string) service.ToolBackend {
		g, idx, ok := srv.GetRepoResources(repo)
		if !ok {
			return nil
		}
		return &query.Backend{Graph: g, Index: idx}
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { srv.Stop() }) //nolint:errcheck

	client := service.NewAutoClient(srv.Addr)
	cli := &CLI{Client: client}

	t.Log("Analyzing with --clone...")

	analyzeCmd := &AnalyzeCmd{
		Targets: []string{"https://github.com/go-git/go-billy"},
		Clone:   true,
		Depth:   1,
		Embed:   "off",
	}
	out := captureStdout(t, func() {
		if err := analyzeCmd.Run(cli); err != nil {
			t.Fatalf("analyze --clone: %v", err)
		}
	})
	t.Log(out)

	if !strings.Contains(out, "clone to disk") {
		t.Errorf("expected 'clone to disk' in output, got:\n%s", out)
	}

	// Verify meta has SourcePath set and no content bucket.
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	entry, ok := registry.Get("go-git/go-billy")
	if !ok {
		t.Fatal("expected 'go-git/go-billy' in registry")
	}

	if entry.Meta.SourcePath == "" {
		t.Error("expected non-empty SourcePath for --clone mode")
	}
	if entry.Meta.HasContentBucket {
		t.Error("expected HasContentBucket=false for --clone mode")
	}
	t.Logf("Meta: commit=%s sourcePath=%s", entry.Meta.CommitHash[:12], entry.Meta.SourcePath)

	srcFile := filepath.Join(entry.Meta.SourcePath, "fs.go")
	if _, err := os.Stat(srcFile); err != nil {
		t.Errorf("expected fs.go on disk at %s: %v", srcFile, err)
	}

	// Query should work after clone-to-disk.
	queryResult, err := client.Query(service.QueryRequest{
		Repo:  "go-git/go-billy",
		Text:  "Filesystem",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	totalResults := len(queryResult.Processes) + len(queryResult.Definitions)
	if totalResults == 0 {
		t.Error("expected query results after clone-to-disk analyze")
	}

	// Source should work (reads from disk, not content bucket).
	sourceResult, err := client.Source(service.SourceRequest{
		Repo:  "go-git/go-billy",
		Files: []string{"fs.go"},
	})
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(sourceResult.Files) > 0 && sourceResult.Files[0].Error != "" {
		t.Errorf("source error: %s", sourceResult.Files[0].Error)
	}
	if len(sourceResult.Files) > 0 && !strings.Contains(sourceResult.Files[0].Content, "package billy") {
		t.Error("expected 'package billy' in source content")
	}

	t.Log("✓ E2E clone-to-disk test passed")
}
