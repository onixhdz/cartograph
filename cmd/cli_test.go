package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"

	"github.com/onixhdz/cartograph/internal/analyze"
	"github.com/onixhdz/cartograph/internal/ingestion"
	"github.com/onixhdz/cartograph/internal/remote"
	"github.com/onixhdz/cartograph/internal/service"
	"github.com/onixhdz/cartograph/internal/storage"
	"github.com/onixhdz/cartograph/internal/storage/bbolt"
)

const (
	testRepo          = "myrepo"
	testPluginDataset = "capec-plugin"
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
	lastSearchReq  service.SearchRequest
	lastContextReq service.ContextRequest
	lastCypherReq  service.CypherRequest
	lastImpactReq  service.ImpactRequest
	lastTreeReq    service.TreeRequest
	lastReloadReq  service.ReloadRequest
}

func (m *mockClient) Query(req service.QueryRequest) (*service.QueryResult, error) {
	m.queryCalled = true
	m.lastQueryReq = req
	if req.Plugin {
		return &service.QueryResult{
			PluginResults: []service.PluginQueryMatch{{
				NodeID: "capec:66",
				Fields: []service.PluginDisplayField{{Label: "Name", Value: "SQL Injection"}},
			}},
		}, nil
	}
	return &service.QueryResult{
		Processes: []service.ProcessMatch{
			{Name: "HandleRequest", Relevance: 0.95},
		},
		Definitions: []service.SymbolMatch{
			{Name: "handler", Label: "Function", FilePath: "server.go", StartLine: 10},
		},
	}, nil
}

func (m *mockClient) Search(req service.SearchRequest) (*service.SearchResult, error) {
	m.lastSearchReq = req
	return &service.SearchResult{
		Repo:         req.Repo,
		Pattern:      req.Pattern,
		FixedStrings: req.FixedStrings,
		IndexStatus:  service.IndexStatusIndexed,
		DurationMS:   4,
		MatchCount:   1,
		FileCount:    1,
		Matches: []service.SearchMatch{{
			FilePath: "internal/query/backend.go",
			Line:     10,
			Column:   5,
			LineText: "results := SearchMulti()",
			Before:   []string{"func run() {"},
			After:    []string{"}"},
		}},
	}, nil
}

func (m *mockClient) Context(req service.ContextRequest) (*service.ContextResult, error) {
	m.contextCalled = true
	m.lastContextReq = req
	result := &service.ContextResult{
		Symbol:  service.SymbolMatch{Name: "Foo", Label: "Function", FilePath: "foo.go", StartLine: 1},
		Callers: []service.SymbolMatch{{Name: "main", Label: "Function", FilePath: "main.go", StartLine: 5}},
		Callees: []service.SymbolMatch{{Name: "bar", Label: "Function", FilePath: "bar.go", StartLine: 3}},
	}
	if req.IncludeRelationships {
		result.RelationshipGroups = []service.RelationshipGroup{{
			Type: "CALLS",
			Relationships: []service.ContextRelationship{{
				From: service.SymbolMatch{Name: "Foo", Label: "Function", FilePath: "foo.go", StartLine: 1},
				To:   service.SymbolMatch{Name: "bar", Label: "Function", FilePath: "bar.go", StartLine: 3},
			}},
		}}
		result.RelationshipStats = &service.RelationshipStats{Depth: req.Depth, ReturnedNodes: 2, ReturnedRelationships: 1, Limit: req.RelationshipLimit}
	}
	return result, nil
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

func (m *mockClient) Cat(req service.CatRequest) (*service.CatResult, error) {
	return &service.CatResult{
		Files: []service.CatFile{
			{Path: "test.go", Content: "package test\n", LineCount: 1},
		},
	}, nil
}

func (m *mockClient) Tree(req service.TreeRequest) (*service.TreeResult, error) {
	m.lastTreeReq = req
	return &service.TreeResult{
		Repo: req.Repo,
		Files: []string{
			"cmd/root.go",
			"cmd/root_test.go",
			"go.mod",
			"internal/service/client.go",
			"internal/service/client_test.go",
			"main.go",
		},
	}, nil
}

func (m *mockClient) Reload(req service.ReloadRequest) error {
	m.reloadCalled = true
	m.lastReloadReq = req
	return nil
}

func (m *mockClient) Health() (*service.HealthResult, error) {
	m.statusCalled = true
	return &service.HealthResult{
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

func (m *mockClient) AnalyzePreflight(req service.AnalyzePreflightRequest) (*service.AnalyzePreflightResult, error) {
	res := service.NewAnalyzePreflightResult(req, nil, nil, false)
	return &res, nil
}

func (m *mockClient) Schema(req service.SchemaRequest) (*service.SchemaResult, error) {
	if req.Repo == "plugin-dataset" {
		return &service.SchemaResult{
			NodeLabels: []service.NodeLabelSummary{{Label: "FooPattern", Count: 10}},
			RelTypes:   []service.RelTypeSummary{{Type: "MITIGATES", Count: 5}},
			Properties: []string{"name_lc", "description_lc", "related_cwes_text"},
			TotalNodes: 10,
			TotalEdges: 5,
		}, nil
	}
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

	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	return buf.String()
}

func TestQueryCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &QueryCmd{
			SearchQuery:    "handle request",
			TargetSelector: TargetSelector{Repo: testRepo},
			Limit:          5,
			Content:        true,
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.queryCalled {
			t.Error("expected Query to be called")
		}
		if mc.lastQueryReq.Repo != testRepo {
			t.Errorf("repo: got %q, want %q", mc.lastQueryReq.Repo, testRepo)
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

func TestSearchCmd(t *testing.T) {
	t.Run("forwards request and prints grouped output", func(t *testing.T) {
		mc := &mockClient{}
		cmd := &SearchCmd{
			Pattern:      "SearchMulti",
			Repo:         testRepo,
			FixedStrings: true,
			IgnoreCase:   true,
			Limit:        50,
			Context:      2,
			Files:        "internal/**/*.go",
		}
		out := captureStdout(t, func() {
			if err := cmd.Run(&CLI{Client: mc}); err != nil {
				t.Fatalf("Run: %v", err)
			}
		})
		if mc.lastSearchReq.Pattern != "SearchMulti" || !mc.lastSearchReq.FixedStrings || !mc.lastSearchReq.IgnoreCase {
			t.Fatalf("request not forwarded: %+v", mc.lastSearchReq)
		}
		if mc.lastSearchReq.Limit != 50 || mc.lastSearchReq.ContextLines != 2 || mc.lastSearchReq.Files != "internal/**/*.go" {
			t.Fatalf("request flags not forwarded: %+v", mc.lastSearchReq)
		}
		for _, want := range []string{"1 matches in 1 files", "internal/query/backend.go", ">   10  results := SearchMulti()"} {
			if !strings.Contains(out, want) {
				t.Fatalf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("prints json", func(t *testing.T) {
		mc := &mockClient{}
		cmd := &SearchCmd{Pattern: "TODO", Repo: testRepo, JSON: true}
		out := captureStdout(t, func() {
			if err := cmd.Run(&CLI{Client: mc}); err != nil {
				t.Fatalf("Run: %v", err)
			}
		})
		if !strings.Contains(out, `"indexStatus": "indexed"`) || !strings.Contains(out, `"filePath": "internal/query/backend.go"`) {
			t.Fatalf("unexpected json output:\n%s", out)
		}
	})
}

func TestContextCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &ContextCmd{
			Name:              "Foo",
			Repo:              testRepo,
			File:              "foo.go",
			UID:               "uid-123",
			Relationships:     true,
			RelationshipLimit: 50,
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.contextCalled {
			t.Error("expected Context to be called")
		}
		if mc.lastContextReq.Repo != testRepo {
			t.Errorf("repo: got %q, want %q", mc.lastContextReq.Repo, testRepo)
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
		if !mc.lastContextReq.IncludeRelationships {
			t.Error("expected relationships to be requested")
		}
		if mc.lastContextReq.RelationshipLimit != 50 {
			t.Errorf("relationship limit: got %d, want 50", mc.lastContextReq.RelationshipLimit)
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
		if !strings.Contains(out, "Relationships:") {
			t.Error("expected output to contain 'Relationships:'")
		}
		if !strings.Contains(out, "Relationship summary:") {
			t.Error("expected output to contain 'Relationship summary:'")
		}
	})
}

func TestImpactCmd(t *testing.T) {
	t.Run("calls client with correct params", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &ImpactCmd{
			Target:    "Foo",
			Repo:      testRepo,
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
		if mc.lastImpactReq.Repo != testRepo {
			t.Errorf("repo: got %q, want %q", mc.lastImpactReq.Repo, testRepo)
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
			Query:          "MATCH (n) RETURN n",
			TargetSelector: TargetSelector{Repo: testRepo},
		}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !mc.cypherCalled {
			t.Error("expected Cypher to be called")
		}
		if mc.lastCypherReq.Repo != testRepo {
			t.Errorf("repo: got %q, want %q", mc.lastCypherReq.Repo, testRepo)
		}
		if mc.lastCypherReq.Query != "MATCH (n) RETURN n" {
			t.Errorf("query: got %q, want %q", mc.lastCypherReq.Query, "MATCH (n) RETURN n")
		}
		if !strings.Contains(out, "Foo") {
			t.Error("expected output to contain 'Foo'")
		}
	})
}

func TestTreeCmd(t *testing.T) {
	t.Run("prints full tree", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &TreeCmd{Repo: testRepo}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if mc.lastTreeReq.Repo != testRepo {
			t.Errorf("repo: got %q, want %q", mc.lastTreeReq.Repo, testRepo)
		}
		for _, want := range []string{"myrepo/", "├── cmd/", "│   ├── root.go", "│   └── root_test.go", "├── internal/", "│   └── service/", "│       ├── client.go", "│       └── client_test.go", "├── go.mod", "└── main.go", "3 directories, 6 files"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("limits depth", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &TreeCmd{Repo: testRepo, Depth: 1}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		if !strings.Contains(out, "│   └── ...") {
			t.Fatalf("expected ellipsis for truncated directory, got:\n%s", out)
		}
		if strings.Contains(out, "client.go") {
			t.Fatalf("did not expect nested file beyond depth, got:\n%s", out)
		}
	})

	t.Run("prints selected directory", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &TreeCmd{Repo: testRepo, Path: "internal/service"}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		for _, want := range []string{"internal/service/", "├── client.go", "└── client_test.go", "0 directories, 2 files"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
		if strings.Contains(out, "root.go") {
			t.Fatalf("did not expect file outside selected directory, got:\n%s", out)
		}
	})

	t.Run("prints selected file with parent", func(t *testing.T) {
		mc := &mockClient{}
		cli := &CLI{Client: mc}
		cmd := &TreeCmd{Repo: testRepo, Path: "internal/service/client.go"}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		for _, want := range []string{"internal/service/", "└── client.go", "0 directories, 1 file"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
		if strings.Contains(out, "client_test.go") {
			t.Fatalf("did not expect sibling file, got:\n%s", out)
		}
	})

	t.Run("errors on missing path", func(t *testing.T) {
		cmd := &TreeCmd{Repo: testRepo, Path: "missing/path"}
		err := cmd.Run(&CLI{Client: &mockClient{}})
		if err == nil {
			t.Fatal("expected missing path error")
		}
		if !strings.Contains(err.Error(), `path "missing/path" not found`) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects negative depth", func(t *testing.T) {
		cmd := &TreeCmd{Repo: testRepo, Depth: -1}
		if err := cmd.Run(&CLI{Client: &mockClient{}}); err == nil {
			t.Fatal("expected error for negative depth")
		}
	})
}

func TestSchemaCmd_PluginGuidance(t *testing.T) {
	mc := &mockClient{}
	cli := &CLI{Client: mc}
	cmd := &SchemaCmd{TargetSelector: TargetSelector{Plugin: "plugin-dataset"}}

	out := captureStdout(t, func() {
		if err := cmd.Run(cli); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	if !strings.Contains(out, "Use plugin-provided references") {
		t.Fatalf("expected plugin guidance in schema output, got: %s", out)
	}
	if !strings.Contains(out, "cypher -p <plugin-dataset>") {
		t.Fatalf("expected generic cypher guidance in schema output, got: %s", out)
	}
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
		// XDG_DATA_HOME to a parent so that dataDir == storage.DefaultDataDir().
		_ = os.Setenv("XDG_DATA_HOME", filepath.Dir(dataDir))
		defer func() { _ = os.Setenv("XDG_DATA_HOME", origXDG) }()

		// Rename the temp dir's base to "cartograph" to match storage.DefaultDataDir().
		actualDataDir := storage.DefaultDataDir()
		_ = os.MkdirAll(actualDataDir, 0o750)

		registry, err := storage.NewRegistry(actualDataDir)
		if err != nil {
			t.Fatalf("create registry: %v", err)
		}
		entry := storage.RegistryEntry{
			Name:      testRepo,
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
		repoDir := filepath.Join(actualDataDir, testRepo, "abc12345")
		_ = os.MkdirAll(repoDir, 0o750)

		cli := &CLI{Client: &mockClient{}}
		cmd := &StatusCmd{Repo: testRepo}

		out := captureStdout(t, func() {
			if err := cmd.Run(cli); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})

		for _, want := range []string{testRepo, "42", "99", "go, python", "1.5s"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected output to contain %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("not indexed repo prints message", func(t *testing.T) {
		dataDir := t.TempDir()
		origXDG := os.Getenv("XDG_DATA_HOME")
		_ = os.Setenv("XDG_DATA_HOME", filepath.Dir(dataDir))
		defer func() { _ = os.Setenv("XDG_DATA_HOME", origXDG) }()

		actualDataDir := storage.DefaultDataDir()
		_ = os.MkdirAll(actualDataDir, 0o750)

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
		_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600)

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
		_ = os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package hello\nfunc Hello() {}\n"), 0o600)

		orig, _ := os.Getwd()
		_ = os.Chdir(dir)
		t.Cleanup(func() { _ = os.Chdir(orig) })

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
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "utils.go"), []byte("package main\nfunc helper() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

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

func TestAnalyzeCmd_RepoSelectionAutoSplitsLocalRepos(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	dir := t.TempDir()
	writeCLITestFile(t, dir, "package.json", `{"name":"root","workspaces":["apps/web","services/worker"]}`)
	writeCLITestFile(t, dir, "apps/web/package.json", `{"name":"web"}`)
	writeCLITestFile(t, dir, "apps/web/index.js", "export function web() {}\n")
	writeCLITestFile(t, dir, "apps/web/view.js", "export function view() {}\n")
	writeCLITestFile(t, dir, "services/worker/go.mod", "module example.com/worker\n")
	writeCLITestFile(t, dir, "services/worker/main.go", "package main\nfunc main() {}\n")
	writeCLITestFile(t, dir, "services/worker/worker.go", "package main\nfunc worker() {}\n")

	mc := &mockClient{}
	cli := &CLI{Client: mc}
	cmd := &AnalyzeCmd{Targets: []string{dir}, Repos: "auto", Embed: "off"}

	out := captureStdout(t, func() {
		if err := cmd.Run(cli); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.Count(out, "Analyzing ") != 2 {
		t.Fatalf("expected two selected repos to be analyzed, got output:\n%s", out)
	}
	reg, err := storage.NewRegistry(storage.DefaultDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Resolve("web"); err != nil {
		t.Fatalf("expected web registry entry: %v", err)
	}
	if _, err := reg.Resolve("example.com/worker"); err != nil {
		t.Fatalf("expected worker registry entry: %v", err)
	}
}

func TestAnalyzeCmd_RepoSelectionNoneKeepsContainer(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	dir := t.TempDir()
	writeCLITestFile(t, dir, "package.json", `{"name":"root","workspaces":["apps/web","apps/admin"]}`)
	writeCLITestFile(t, dir, "apps/web/package.json", `{"name":"web"}`)
	writeCLITestFile(t, dir, "apps/web/index.js", "export function web() {}\n")
	writeCLITestFile(t, dir, "apps/web/view.js", "export function view() {}\n")
	writeCLITestFile(t, dir, "apps/admin/package.json", `{"name":"admin"}`)
	writeCLITestFile(t, dir, "apps/admin/index.js", "export function admin() {}\n")
	writeCLITestFile(t, dir, "apps/admin/view.js", "export function view() {}\n")

	mc := &mockClient{}
	cli := &CLI{Client: mc}
	cmd := &AnalyzeCmd{Targets: []string{dir}, Repos: "none", Embed: "off"}
	out := captureStdout(t, func() {
		if err := cmd.Run(cli); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if strings.Count(out, "Analyzing ") != 1 {
		t.Fatalf("expected one container analysis, got output:\n%s", out)
	}
}

func TestAnalyzeCmd_RepoSelectionRefusesLinkedContainer(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	dir := t.TempDir()
	writeCLITestFile(t, dir, "package.json", `{"name":"root","workspaces":["apps/web","apps/admin"]}`)
	writeCLITestFile(t, dir, "apps/web/package.json", `{"name":"web"}`)
	writeCLITestFile(t, dir, "apps/web/index.js", "export function web() {}\n")
	writeCLITestFile(t, dir, "apps/web/view.js", "export function view() {}\n")
	writeCLITestFile(t, dir, "apps/admin/package.json", `{"name":"admin"}`)
	writeCLITestFile(t, dir, "apps/admin/index.js", "export function admin() {}\n")
	writeCLITestFile(t, dir, "apps/admin/view.js", "export function view() {}\n")

	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := storage.NewRegistry(storage.DefaultDataDir())
	if err != nil {
		t.Fatal(err)
	}
	containerHash := analyze.ShortHash(abs)
	if err := reg.Add(storage.RegistryEntry{Name: filepath.Base(abs), Path: abs, Hash: containerHash, LinkedRepos: []string{"linked"}}); err != nil {
		t.Fatal(err)
	}

	cmd := &AnalyzeCmd{Targets: []string{dir}, Repos: "auto", Embed: "off"}
	err = cmd.Run(&CLI{Client: &mockClient{}})
	if err == nil || !strings.Contains(err.Error(), "already indexed") {
		t.Fatalf("expected linked container refusal, got %v", err)
	}
}

func TestRenderDetectedReposAlignsTable(t *testing.T) {
	candidates := []ingestion.RepoCandidate{
		{RelPath: "short", Classification: ingestion.RepoClassificationPrimary, Recommended: true, Signals: []ingestion.RepoSignal{ingestion.RepoSignalGitRoot}},
		{RelPath: "very/long/repo/path", Classification: ingestion.RepoClassificationPrimary, Recommended: true, Signals: []ingestion.RepoSignal{ingestion.RepoSignalManifestRoot, ingestion.RepoSignalSourceDensity}},
	}

	out := captureStdout(t, func() {
		renderDetectedRepos("/repo", candidates)
	})
	if !strings.Contains(out, "REPO") || !strings.Contains(out, "STATUS") || !strings.Contains(out, "SIGNALS") {
		t.Fatalf("expected table headers, got:\n%s", out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "short") || strings.Contains(line, "very/long/repo/path") {
			if !regexp.MustCompile(`^  \S+(?:\s{2,})recommended(?:\s{2,})\S+`).MatchString(line) {
				t.Fatalf("expected aligned table row, got %q in output:\n%s", line, out)
			}
		}
	}
}

func TestRenderDetectedReposLimitsPreviewRows(t *testing.T) {
	candidates := make([]ingestion.RepoCandidate, detectedReposPreviewRows+2)
	for i := range candidates {
		candidates[i] = ingestion.RepoCandidate{
			RelPath:        fmt.Sprintf("repo-%02d", i),
			Classification: ingestion.RepoClassificationPrimary,
			Recommended:    true,
			Signals:        []ingestion.RepoSignal{ingestion.RepoSignalGitRoot},
		}
	}

	out := captureStdout(t, func() {
		renderDetectedRepos("/repo", candidates)
	})
	if strings.Contains(out, "repo-20") || strings.Contains(out, "repo-21") {
		t.Fatalf("expected preview to hide rows after limit, got:\n%s", out)
	}
	if !strings.Contains(out, "... and 2 more repo candidates") {
		t.Fatalf("expected truncated count, got:\n%s", out)
	}
}

func TestSplitRemoteRepoHashIncludesResolvedBranch(t *testing.T) {
	identity := remote.RepoIdentity{Canonical: "github.com/acme/repo"}
	mainHash := splitRemoteRepoHash(identity, "apps/web", "main")
	v1Hash := splitRemoteRepoHash(identity, "apps/web", "v1.0.0")
	if mainHash == v1Hash {
		t.Fatal("expected different split repo hashes for different branches")
	}

	qualified := remote.RepoIdentity{Canonical: "github.com/acme/repo@v2.0.0"}
	if got, want := splitRemoteRepoHash(qualified, "apps/web", "main"), splitRemoteRepoHash(qualified, "apps/web", "v2.0.0"); got != want {
		t.Fatalf("expected canonical inline ref to remain authoritative, got %s and %s", got, want)
	}
}

func TestPopulateContentBucketScopesKeys(t *testing.T) {
	fs := memfs.New()
	writeMemCLITestFile(t, fs, "apps/web/index.js", "export function web() {}\n")
	writeMemCLITestFile(t, fs, "apps/web/src/view.js", "export function view() {}\n")

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := bbolt.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := bbolt.NewContentStoreFromDB(store.DB())
	if err != nil {
		t.Fatal(err)
	}
	count, err := populateContentBucket(cs, fs, "/apps/web")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("content count = %d, want 2", count)
	}
	if _, err := cs.Get("index.js"); err != nil {
		t.Fatalf("expected project-relative index.js content: %v", err)
	}
	if _, err := cs.Get("src/view.js"); err != nil {
		t.Fatalf("expected project-relative src/view.js content: %v", err)
	}
	if _, err := cs.Get("apps/web/index.js"); err == nil {
		t.Fatal("did not expect container-relative content key")
	}
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeCLITestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeMemCLITestFile(t *testing.T, fs billy.Filesystem, rel, content string) {
	t.Helper()
	if err := fs.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := fs.Create(rel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNilClient(t *testing.T) {
	cli := &CLI{}

	cmds := []struct {
		name string
		run  func() error
	}{
		{"Query", func() error {
			return (&QueryCmd{SearchQuery: "test", TargetSelector: TargetSelector{Repo: "r"}}).Run(cli)
		}},
		{"Context", func() error { return (&ContextCmd{Name: "Foo", Repo: "r"}).Run(cli) }},
		{"Impact", func() error { return (&ImpactCmd{Target: "Foo", Repo: "r"}).Run(cli) }},
		{"Cypher", func() error { return (&CypherCmd{Query: "q", TargetSelector: TargetSelector{Repo: "r"}}).Run(cli) }},
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

func TestQueryCmdPluginTarget(t *testing.T) {
	mc := &mockClient{}
	cli := &CLI{Client: mc}
	cmd := &QueryCmd{SearchQuery: "sql injection", TargetSelector: TargetSelector{Plugin: testPluginDataset}}

	out := captureStdout(t, func() {
		if err := cmd.Run(cli); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})

	if !mc.queryCalled {
		t.Fatal("expected query to be called")
	}
	if !mc.lastQueryReq.Plugin {
		t.Fatal("expected plugin query flag to be set")
	}
	if mc.lastQueryReq.Repo != testPluginDataset {
		t.Fatalf("repo = %q, want %q", mc.lastQueryReq.Repo, testPluginDataset)
	}
	if !strings.Contains(out, "Name: SQL Injection") {
		t.Fatalf("expected plugin result output, got %q", out)
	}
}
