package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/storage"
)

type mockClient struct {
	queryCalled    bool
	contextCalled  bool
	cypherCalled   bool
	impactCalled   bool
	statusCalled   bool
	reloadCalled   bool
	shutdownCalled bool

	lastQueryReq   service.QueryRequest
	lastContextReq service.ContextRequest
	lastCypherReq  service.CypherRequest
	lastImpactReq  service.ImpactRequest
	lastReloadReq  service.ReloadRequest
}

func (m *mockClient) Query(req service.QueryRequest) (*service.QueryResult, error) {
	m.queryCalled = true
	m.lastQueryReq = req
	return &service.QueryResult{
		Processes: []service.ProcessMatch{
			{Name: "HandleRequest", Relevance: 0.95},
		},
		Definitions: []service.SymbolMatch{
			{Name: "handler", Label: "Function", FilePath: "server.go", StartLine: 10},
		},
	}, nil
}

func (m *mockClient) Context(req service.ContextRequest) (*service.ContextResult, error) {
	m.contextCalled = true
	m.lastContextReq = req
	return &service.ContextResult{
		Symbol:  service.SymbolMatch{Name: "Foo", Label: "Function", FilePath: "foo.go", StartLine: 1},
		Callers: []service.SymbolMatch{{Name: "main", Label: "Function", FilePath: "main.go", StartLine: 5}},
		Callees: []service.SymbolMatch{{Name: "bar", Label: "Function", FilePath: "bar.go", StartLine: 3}},
	}, nil
}

func (m *mockClient) Cypher(req service.CypherRequest) (*service.CypherResult, error) {
	m.cypherCalled = true
	m.lastCypherReq = req
	return &service.CypherResult{
		Columns: []string{"name", "label"},
		Rows: []map[string]any{
			{"name": "Foo", "label": "Function"},
		},
	}, nil
}

func (m *mockClient) Impact(req service.ImpactRequest) (*service.ImpactResult, error) {
	m.impactCalled = true
	m.lastImpactReq = req
	return &service.ImpactResult{
		Target:   service.SymbolMatch{Name: "Foo", Label: "Function", FilePath: "foo.go", StartLine: 1},
		Affected: []service.SymbolMatch{{Name: "bar", Label: "Function", FilePath: "bar.go", StartLine: 3}},
		Depth:    5,
	}, nil
}

func (m *mockClient) Source(req service.SourceRequest) (*service.SourceResult, error) {
	return &service.SourceResult{
		Files: []service.SourceFile{
			{Path: "test.go", Content: "package test\n", LineCount: 1},
		},
	}, nil
}

func (m *mockClient) Reload(req service.ReloadRequest) error {
	m.reloadCalled = true
	m.lastReloadReq = req
	return nil
}

func (m *mockClient) Status() (*service.StatusResult, error) {
	m.statusCalled = true
	return &service.StatusResult{
		Running: true,
		LoadedRepos: []service.RepoStatus{
			{Name: "cartograph", NodeCount: 100, EdgeCount: 200},
			{Name: "other-repo", NodeCount: 50, EdgeCount: 75},
		},
		Uptime: "1h30m",
	}, nil
}

func (m *mockClient) Shutdown() error {
	m.shutdownCalled = true
	return nil
}

func (m *mockClient) Embed(_ service.EmbedRequest) (*service.EmbedStatusResult, error) {
	return &service.EmbedStatusResult{Status: "pending"}, nil
}

func (m *mockClient) EmbedStatus(_ service.EmbedStatusRequest) (*service.EmbedStatusResult, error) {
	return &service.EmbedStatusResult{Status: ""}, nil
}

func (m *mockClient) Schema(req service.SchemaRequest) (*service.SchemaResult, error) {
	return &service.SchemaResult{
		NodeLabels: []service.NodeLabelSummary{
			{Label: "Function", Count: 50},
			{Label: "File", Count: 20},
		},
		RelTypes: []service.RelTypeSummary{
			{Type: "CALLS", Count: 100},
		},
		Properties: []string{"id", "name", "filePath"},
		TotalNodes: 70,
		TotalEdges: 100,
	}, nil
}

// captureStdout captures everything written to os.Stdout during fn().
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()
	return buf.String()
}

func TestQueryCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &QueryCmd{
			SearchQuery: "handle request",
			Repo:        "myrepo",
			Limit:       5,
			Content:     true,
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.queryCalled {
			t.Error("expected Query to be called")
		}
		if mc.lastQueryReq.Repo != "myrepo" {
			t.Errorf("repo: got %q, want %q", mc.lastQueryReq.Repo, "myrepo")
		}
		if mc.lastQueryReq.Text != "handle request" {
			t.Errorf("text: got %q, want %q", mc.lastQueryReq.Text, "handle request")
		}
		if mc.lastQueryReq.Limit != 5 {
			t.Errorf("limit: got %d, want 5", mc.lastQueryReq.Limit)
		}
		if !mc.lastQueryReq.Content {
			t.Error("content: expected true")
		}
		if !strings.Contains(out, "Processes:") {
			t.Error("expected output to contain 'Processes:'")
		}
		if !strings.Contains(out, "Definitions:") {
			t.Error("expected output to contain 'Definitions:'")
		}
	})
}

func TestContextCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &ContextCmd{
			Name: "Foo",
			Repo: "myrepo",
			File: "foo.go",
			UID:  "uid-123",
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.contextCalled {
			t.Error("expected Context to be called")
		}
		if mc.lastContextReq.Repo != "myrepo" {
			t.Errorf("repo: got %q, want %q", mc.lastContextReq.Repo, "myrepo")
		}
		if mc.lastContextReq.Name != "Foo" {
			t.Errorf("name: got %q, want %q", mc.lastContextReq.Name, "Foo")
		}
		if mc.lastContextReq.File != "foo.go" {
			t.Errorf("file: got %q, want %q", mc.lastContextReq.File, "foo.go")
		}
		if mc.lastContextReq.UID != "uid-123" {
			t.Errorf("uid: got %q, want %q", mc.lastContextReq.UID, "uid-123")
		}
		if !strings.Contains(out, "Symbol:") {
			t.Error("expected output to contain 'Symbol:'")
		}
		if !strings.Contains(out, "Callers:") {
			t.Error("expected output to contain 'Callers:'")
		}
		if !strings.Contains(out, "Callees:") {
			t.Error("expected output to contain 'Callees:'")
		}
	})
}

func TestImpactCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &ImpactCmd{
			Target:    "Foo",
			Repo:      "myrepo",
			Direction: "downstream",
			Depth:     3,
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.impactCalled {
			t.Error("expected Impact to be called")
		}
		if mc.lastImpactReq.Repo != "myrepo" {
			t.Errorf("repo: got %q, want %q", mc.lastImpactReq.Repo, "myrepo")
		}
		if mc.lastImpactReq.Target != "Foo" {
			t.Errorf("target: got %q, want %q", mc.lastImpactReq.Target, "Foo")
		}
		if mc.lastImpactReq.Direction != "downstream" {
			t.Errorf("direction: got %q, want %q", mc.lastImpactReq.Direction, "downstream")
		}
		if mc.lastImpactReq.Depth != 3 {
			t.Errorf("depth: got %d, want 3", mc.lastImpactReq.Depth)
		}
		if !strings.Contains(out, "Target:") {
			t.Error("expected output to contain 'Target:'")
		}
		if !strings.Contains(out, "Affected") {
			t.Error("expected output to contain 'Affected'")
		}
	})
}

func TestCypherCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &CypherCmd{
			Query: "MATCH (n) RETURN n",
			Repo:  "myrepo",
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.cypherCalled {
			t.Error("expected Cypher to be called")
		}
		if mc.lastCypherReq.Repo != "myrepo" {
			t.Errorf("repo: got %q, want %q", mc.lastCypherReq.Repo, "myrepo")
		}
		if mc.lastCypherReq.Query != "MATCH (n) RETURN n" {
			t.Errorf("query: got %q, want %q", mc.lastCypherReq.Query, "MATCH (n) RETURN n")
		}
		if !strings.Contains(out, "Foo") {
			t.Error("expected output to contain 'Foo'")
		}
	})
}

func TestListCmd(t *testing.T) {
	t.Run("reads registry and prints table", func(t *testing.T) {
		// Set up a temporary data dir with a registry.
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)

		dataDir := filepath.Join(tmpDir, "cartograph")
		registry, err := storage.NewRegistry(dataDir)
		if err != nil {
			t.Fatalf("create registry: %v", err)
		}
		now := time.Now()
		if err := registry.Add(storage.RegistryEntry{
			Name:      "my-project",
			Path:      "/tmp/my-project",
			Hash:      "abc12345",
			IndexedAt: now.Add(-2 * time.Minute),
			NodeCount: 100,
			EdgeCount: 200,
		}); err != nil {
			t.Fatal(err)
		}
		if err := registry.Add(storage.RegistryEntry{
			Name:      "gorilla/mux",
			Path:      "https://github.com/gorilla/mux",
			Hash:      "def67890",
			IndexedAt: now.Add(-1 * time.Hour),
			NodeCount: 50,
			EdgeCount: 75,
			URL:       "github.com/gorilla/mux",
		}); err != nil {
			t.Fatal(err)
		}

		cli := &CLI{}
		cmd := &ListCmd{}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "my-project") {
			t.Error("expected output to contain 'my-project'")
		}
		if !strings.Contains(out, "gorilla/mux") {
			t.Error("expected output to contain 'gorilla/mux'")
		}
		if !strings.Contains(out, "local") {
			t.Error("expected output to contain 'local' type")
		}
		if !strings.Contains(out, "url") {
			t.Error("expected output to contain 'url' type")
		}
		if !strings.Contains(out, "ago") {
			t.Error("expected output to contain time-ago string")
		}
		if !strings.Contains(out, "Name") {
			t.Error("expected output to contain header 'Name'")
		}
		if !strings.Contains(out, "Embedding") {
			t.Error("expected output to contain header 'Embedding'")
		}
		if !strings.Contains(out, "none") {
			t.Error("expected output to show 'none' embedding status")
		}
	})

	t.Run("empty registry shows message", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)

		cli := &CLI{}
		cmd := &ListCmd{}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "No indexed repositories") {
			t.Error("expected 'No indexed repositories' message")
		}
	})

	t.Run("shows embedding status column", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)

		dataDir := filepath.Join(tmpDir, "cartograph")
		registry, err := storage.NewRegistry(dataDir)
		if err != nil {
			t.Fatalf("create registry: %v", err)
		}
		if err := registry.Add(storage.RegistryEntry{
			Name:      "embed-project",
			Path:      "/tmp/embed-project",
			Hash:      "aaa11111",
			IndexedAt: time.Now().Add(-5 * time.Minute),
			NodeCount: 42,
			EdgeCount: 80,
			Meta: storage.Meta{
				EmbeddingStatus:   "complete",
				EmbeddingModel:    "nomic-embed-code",
				EmbeddingDims:     768,
				EmbeddingNodes:    42,
				EmbeddingTotal:    42,
				EmbeddingDuration: "3.2s",
			},
		}); err != nil {
			t.Fatal(err)
		}
		if err := registry.Add(storage.RegistryEntry{
			Name:      "no-embed",
			Path:      "/tmp/no-embed",
			Hash:      "bbb22222",
			IndexedAt: time.Now().Add(-10 * time.Minute),
			NodeCount: 10,
			EdgeCount: 15,
		}); err != nil {
			t.Fatal(err)
		}

		cli := &CLI{}
		cmd := &ListCmd{}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "Embedding") {
			t.Error("expected output to contain 'Embedding' header")
		}
		if !strings.Contains(out, "complete (42 nodes") {
			t.Error("expected output to show completed embedding node count")
		}
		if !strings.Contains(out, "3.2s") {
			t.Error("expected output to show embedding duration")
		}
		if !strings.Contains(out, "none") {
			t.Error("expected output to show 'none' for repo without embedding")
		}
	})
}

func TestTimeAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s ago"},
		{30 * time.Second, "30s ago"},
		{2 * time.Minute, "2m ago"},
		{45 * time.Minute, "45m ago"},
		{2 * time.Hour, "2h ago"},
		{48 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := timeAgo(time.Now().Add(-tt.d))
			if got != tt.want {
				t.Errorf("timeAgo(-%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}

	t.Run("zero time", func(t *testing.T) {
		if got := timeAgo(time.Time{}); got != "unknown" {
			t.Errorf("timeAgo(zero) = %q, want %q", got, "unknown")
		}
	})
}

func TestStatusCmd(t *testing.T) {
	t.Run("shows index info from disk registry", func(t *testing.T) {
		// Set up a temporary data directory with a registry entry + meta.
		dataDir := t.TempDir()
		origXDG := os.Getenv("XDG_DATA_HOME")
		// DefaultDataDir appends "cartograph" to XDG_DATA_HOME, so point
		// XDG_DATA_HOME to a parent so that dataDir == DefaultDataDir().
		os.Setenv("XDG_DATA_HOME", filepath.Dir(dataDir))
		defer os.Setenv("XDG_DATA_HOME", origXDG)

		// Rename the temp dir's base to "cartograph" to match DefaultDataDir().
		actualDataDir := DefaultDataDir()
		os.MkdirAll(actualDataDir, 0o755) //nolint:errcheck

		registry, err := storage.NewRegistry(actualDataDir)
		if err != nil {
			t.Fatalf("create registry: %v", err)
		}
		entry := storage.RegistryEntry{
			Name:      "myrepo",
			Path:      "/fake/path/myrepo",
			Hash:      "abc12345",
			IndexedAt: time.Now(),
			NodeCount: 42,
			EdgeCount: 99,
			Meta: storage.Meta{
				Languages: []string{"go", "python"},
				Duration:  "1.5s",
			},
		}
		if err := registry.Add(entry); err != nil {
			t.Fatalf("add registry entry: %v", err)
		}

		// Create repo data dir so status can report artifact sizes.
		repoDir := filepath.Join(actualDataDir, "myrepo", "abc12345")
		os.MkdirAll(repoDir, 0o755) //nolint:errcheck

		cli := &CLI{Client: &mockClient{}}
		cmd := &StatusCmd{Repo: "myrepo"}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		for _, want := range []string{"myrepo", "42", "99", "go, python", "1.5s"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("not indexed repo prints message", func(t *testing.T) {
		dataDir := t.TempDir()
		origXDG := os.Getenv("XDG_DATA_HOME")
		os.Setenv("XDG_DATA_HOME", filepath.Dir(dataDir))
		defer os.Setenv("XDG_DATA_HOME", origXDG)

		actualDataDir := DefaultDataDir()
		os.MkdirAll(actualDataDir, 0o755) //nolint:errcheck

		cli := &CLI{Client: &mockClient{}}
		cmd := &StatusCmd{Repo: "nonexistent"}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "not indexed") {
			t.Errorf("expected 'not indexed' message, got:\n%s", out)
		}
	})
}

func TestAnalyzeCmd(t *testing.T) {
	t.Run("analyzes a temp directory", func(t *testing.T) {
		// Create a temp dir with some Go files.
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644) //nolint:errcheck

		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &AnalyzeCmd{Targets: []string{dir}, Embed: "off"}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "Analyzing") {
			t.Error("expected output to contain 'Analyzing'")
		}
		if !strings.Contains(out, "Graph:") {
			t.Error("expected output to contain 'Graph:'")
		}
		if !strings.Contains(out, "Done in") {
			t.Error("expected output to contain 'Done in'")
		}

		// Should have notified service to reload.
		if !mc.reloadCalled {
			t.Error("expected Reload to be called")
		}
	})

	t.Run("defaults to current dir", func(t *testing.T) {
		// Use a temp dir as CWD instead of the whole repo to avoid
		// heavy ingestion + memory pressure that can crash the container.
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package hello\nfunc Hello() {}\n"), 0o644) //nolint:errcheck

		orig, _ := os.Getwd()
		os.Chdir(dir)                        //nolint:errcheck
		t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck

		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &AnalyzeCmd{Embed: "off"}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "Analyzing") {
			t.Error("expected output to contain 'Analyzing'")
		}
	})

	t.Run("returns error for nonexistent path", func(t *testing.T) {
		cli := &CLI{}
		cmd := &AnalyzeCmd{Targets: []string{"/nonexistent/path/that/does/not/exist"}, Embed: "off"}
		err := cmd.Run(cli)
		if err == nil {
			t.Error("expected error for nonexistent path")
		}
	})
}

func TestCleanCmd(t *testing.T) {
	t.Run("prints message for unknown repo", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &CleanCmd{}

		// CleanCmd.Run calls detectRepo which reads the cwd.
		// Running in the test dir should detect the current git repo or fail gracefully.
		out := captureStdout(t, func() {
			_ = cmd.Run(cli)
		})

		// Either "No index found" or "Cleaned index" depending on state.
		if out == "" {
			t.Error("expected some output from clean command")
		}
	})

	t.Run("prints message for unknown repo by name", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &CleanCmd{Repo: "nonexistent-repo-xyz"}

		out := captureStdout(t, func() {
			_ = cmd.Run(cli)
		})

		if !strings.Contains(out, "No index found") {
			t.Errorf("expected 'No index found' in output, got %q", out)
		}
	})

	t.Run("clean --all prints cleaning message", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &CleanCmd{All: true}

		out := captureStdout(t, func() {
			_ = cmd.Run(cli)
		})

		if !strings.Contains(out, "Cleaning all indexes") {
			t.Errorf("expected 'Cleaning all indexes' in output, got %q", out)
		}
		if !strings.Contains(out, "Done.") {
			t.Errorf("expected 'Done.' in output, got %q", out)
		}
	})
}

func TestAnalyzeCmd_MultipleSources(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "utils.go"), []byte("package main\nfunc helper() {}\n"), 0o644)

	mc := &mockClient{}
	cli := &CLI{Client: mc}
	cmd := &AnalyzeCmd{Targets: []string{dir}, Embed: "off"}

	out := captureStdout(t, func() {
		if err := cmd.Run(cli); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "nodes") {
		t.Error("expected output to mention nodes")
	}
	if !strings.Contains(out, "edges") {
		t.Error("expected output to mention edges")
	}
}

func TestNilClient(t *testing.T) {
	cli := &CLI{}

	cmds := []struct {
		name string
		run  func() error
	}{
		{"Query", func() error { return (&QueryCmd{SearchQuery: "test", Repo: "r"}).Run(cli) }},
		{"Context", func() error { return (&ContextCmd{Name: "Foo", Repo: "r"}).Run(cli) }},
		{"Impact", func() error { return (&ImpactCmd{Target: "Foo", Repo: "r"}).Run(cli) }},
		{"Cypher", func() error { return (&CypherCmd{Query: "q", Repo: "r"}).Run(cli) }},
	}

	for _, tc := range cmds {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				if err := tc.run(); err != nil {
					t.Fatalf("unexpected error for nil client: %v", err)
				}
			})
			if !strings.Contains(out, errNoService) {
				t.Errorf("expected no-service message, got %q", out)
			}
		})
	}
}
