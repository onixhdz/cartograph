package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/onixhdz/cartograph/internal/service"
)

const (
	mcpTestRepo       = "myrepo"
	mcpTestPluginRepo = "mitre-cwe" //nolint:misspell // MITRE is the organization name.
)

// mockClient implements the Client interface with canned responses
// for testing the MCP tool handlers without needing a real graph.
type mockClient struct {
	queryResult   *service.QueryResult
	searchResult  *service.SearchResult
	contextResult *service.ContextResult
	impactResult  *service.ImpactResult
	cypherResult  *service.CypherResult
	catResult     *service.CatResult
	treeResult    *service.TreeResult
	schemaResult  *service.SchemaResult
	healthResult  *service.HealthResult
	statusResult  *service.StatusResult
	listResult    *service.ListResult
	err           error

	// capture last request for assertions
	lastQueryReq   service.QueryRequest
	lastSearchReq  service.SearchRequest
	lastContextReq service.ContextRequest
	lastImpactReq  service.ImpactRequest
	lastCypherReq  service.CypherRequest
	lastCatReq     service.CatRequest
	lastTreeReq    service.TreeRequest
	lastSchemaReq  service.SchemaRequest
	lastStatusReq  service.StatusRequest
}

func (m *mockClient) Query(req service.QueryRequest) (*service.QueryResult, error) {
	m.lastQueryReq = req
	return m.queryResult, m.err
}

func (m *mockClient) Search(req service.SearchRequest) (*service.SearchResult, error) {
	m.lastSearchReq = req
	return m.searchResult, m.err
}

func (m *mockClient) Context(req service.ContextRequest) (*service.ContextResult, error) {
	m.lastContextReq = req
	return m.contextResult, m.err
}

func (m *mockClient) Cypher(req service.CypherRequest) (*service.CypherResult, error) {
	m.lastCypherReq = req
	return m.cypherResult, m.err
}

func (m *mockClient) Impact(req service.ImpactRequest) (*service.ImpactResult, error) {
	m.lastImpactReq = req
	return m.impactResult, m.err
}

func (m *mockClient) Cat(req service.CatRequest) (*service.CatResult, error) {
	m.lastCatReq = req
	return m.catResult, m.err
}

func (m *mockClient) Tree(req service.TreeRequest) (*service.TreeResult, error) {
	m.lastTreeReq = req
	return m.treeResult, m.err
}

func (m *mockClient) Schema(req service.SchemaRequest) (*service.SchemaResult, error) {
	m.lastSchemaReq = req
	return m.schemaResult, m.err
}

func (m *mockClient) Health() (*service.HealthResult, error) {
	return m.healthResult, m.err
}

func (m *mockClient) Status(req service.StatusRequest) (*service.StatusResult, error) {
	m.lastStatusReq = req
	return m.statusResult, m.err
}

func (m *mockClient) List() (*service.ListResult, error) {
	return m.listResult, m.err
}

// connectTestServer creates an MCP server with the given mock client,
// connects a client to it via in-memory transport, and returns the
// client session. The caller should defer session.Close().
func connectTestServer(t *testing.T, mock *mockClient) *sdkmcp.ClientSession {
	t.Helper()

	srv := NewServer("test", mock)
	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "test-client", Version: "v0.0.1"},
		nil,
	)

	ctx := context.Background()
	t1, t2 := sdkmcp.NewInMemoryTransports()

	if _, err := srv.server.Connect(ctx, t1, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	return session
}

func TestToolsList(t *testing.T) {
	mock := &mockClient{
		healthResult: &service.HealthResult{Running: true},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	expectedTools := map[string]bool{
		"cartograph_query":   false,
		"cartograph_search":  false,
		"cartograph_context": false,
		"cartograph_impact":  false,
		"cartograph_cypher":  false,
		"cartograph_cat":     false,
		"cartograph_tree":    false,
		"cartograph_schema":  false,
		"cartograph_status":  false,
		"cartograph_health":  false,
		"cartograph_list":    false,
	}

	for _, tool := range tools.Tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
		} else {
			expectedTools[tool.Name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestQueryTool(t *testing.T) {
	mock := &mockClient{
		queryResult: &service.QueryResult{
			Processes: []service.ProcessMatch{
				{Name: "handleRequest", Relevance: 0.95},
			},
			Definitions: []service.SymbolMatch{
				{Name: "handleRequest", FilePath: "server.go", StartLine: 42, Label: "Function"},
			},
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_query",
		Arguments: map[string]any{"repo": mcpTestRepo, "query": "HTTP handler", "limit": float64(5)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastQueryReq.Repo != mcpTestRepo {
		t.Errorf("repo = %q, want %q", mock.lastQueryReq.Repo, mcpTestRepo)
	}
	if mock.lastQueryReq.Text != "HTTP handler" {
		t.Errorf("text = %q, want %q", mock.lastQueryReq.Text, "HTTP handler")
	}
	if mock.lastQueryReq.Limit != 5 {
		t.Errorf("limit = %d, want %d", mock.lastQueryReq.Limit, 5)
	}
	if mock.lastQueryReq.Plugin {
		t.Error("plugin = true, want false")
	}

	text := extractText(t, res)
	var result service.QueryResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.Processes) != 1 || result.Processes[0].Name != "handleRequest" {
		t.Errorf("unexpected processes: %+v", result.Processes)
	}
}

func TestQueryToolPluginTarget(t *testing.T) {
	mock := &mockClient{
		queryResult: &service.QueryResult{
			PluginResults: []service.PluginQueryMatch{{
				EntityLabel: "CWEWeakness",
				NodeID:      "cwe:weakness:CWE-918",
				Score:       1,
				Fields: []service.PluginDisplayField{{
					Label: "CWE",
					Value: "CWE-918",
				}},
			}},
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "cartograph_query",
		Arguments: map[string]any{
			"repo":   mcpTestPluginRepo,
			"plugin": true,
			"query":  "SSRF",
			"limit":  float64(5),
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastQueryReq.Repo != mcpTestPluginRepo {
		t.Errorf("repo = %q, want %q", mock.lastQueryReq.Repo, mcpTestPluginRepo)
	}
	if !mock.lastQueryReq.Plugin {
		t.Error("plugin = false, want true")
	}
	if mock.lastQueryReq.Text != "SSRF" {
		t.Errorf("text = %q, want %q", mock.lastQueryReq.Text, "SSRF")
	}
	if mock.lastQueryReq.Limit != 5 {
		t.Errorf("limit = %d, want %d", mock.lastQueryReq.Limit, 5)
	}

	text := extractText(t, res)
	var result service.QueryResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.PluginResults) != 1 || result.PluginResults[0].NodeID != "cwe:weakness:CWE-918" {
		t.Errorf("unexpected plugin results: %+v", result.PluginResults)
	}
}

func TestSearchTool(t *testing.T) {
	mock := &mockClient{
		searchResult: &service.SearchResult{
			Repo:         mcpTestRepo,
			Pattern:      "SearchMulti",
			FixedStrings: true,
			IndexStatus:  service.IndexStatusIndexed,
			MatchCount:   1,
			FileCount:    1,
			Matches: []service.SearchMatch{{
				FilePath: "internal/query/backend.go",
				Line:     10,
				Column:   5,
				LineText: "results := SearchMulti()",
			}},
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "cartograph_search",
		Arguments: map[string]any{
			"repo":         mcpTestRepo,
			"pattern":      "SearchMulti",
			"fixedStrings": true,
			"ignoreCase":   true,
			"limit":        float64(5),
			"context":      float64(2),
			"files":        "internal/**/*.go",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	if mock.lastSearchReq.Repo != mcpTestRepo || mock.lastSearchReq.Pattern != "SearchMulti" {
		t.Fatalf("request not forwarded: %+v", mock.lastSearchReq)
	}
	if !mock.lastSearchReq.FixedStrings || !mock.lastSearchReq.IgnoreCase || mock.lastSearchReq.Limit != 5 || mock.lastSearchReq.ContextLines != 2 {
		t.Fatalf("flags not forwarded: %+v", mock.lastSearchReq)
	}

	text := extractText(t, res)
	var result service.SearchResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.IndexStatus != service.IndexStatusIndexed || len(result.Matches) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestContextTool(t *testing.T) {
	mock := &mockClient{
		contextResult: &service.ContextResult{
			Symbol: service.SymbolMatch{Name: "Serve", FilePath: "server.go", Label: "Function"},
			Callers: []service.SymbolMatch{
				{Name: "main", FilePath: "main.go", Label: "Function"},
			},
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_context",
		Arguments: map[string]any{"repo": mcpTestRepo, "symbol": "Serve"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastContextReq.Repo != mcpTestRepo {
		t.Errorf("repo = %q, want %q", mock.lastContextReq.Repo, mcpTestRepo)
	}
	if mock.lastContextReq.Name != "Serve" {
		t.Errorf("name = %q, want %q", mock.lastContextReq.Name, "Serve")
	}
}

func TestImpactTool(t *testing.T) {
	mock := &mockClient{
		impactResult: &service.ImpactResult{
			Target:   service.SymbolMatch{Name: "Connect", FilePath: "conn.go", Label: "Function"},
			Affected: []service.SymbolMatch{{Name: "Serve", FilePath: "server.go", Label: "Function"}},
			Depth:    3,
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_impact",
		Arguments: map[string]any{"repo": mcpTestRepo, "target": "Connect"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastImpactReq.Direction != "downstream" {
		t.Errorf("direction = %q, want %q", mock.lastImpactReq.Direction, "downstream")
	}
	if mock.lastImpactReq.Depth != 3 {
		t.Errorf("depth = %d, want %d", mock.lastImpactReq.Depth, 3)
	}
}

func TestCypherTool(t *testing.T) {
	mock := &mockClient{
		cypherResult: &service.CypherResult{
			Columns: []string{"name"},
			Rows:    []map[string]any{{"name": "main"}},
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_cypher",
		Arguments: map[string]any{"repo": mcpTestRepo, "query": "MATCH (n:Function) RETURN n.name LIMIT 1"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastCypherReq.Query != "MATCH (n:Function) RETURN n.name LIMIT 1" {
		t.Errorf("query = %q, want match", mock.lastCypherReq.Query)
	}
}

func TestCatTool(t *testing.T) {
	mock := &mockClient{
		catResult: &service.CatResult{
			Files: []service.CatFile{
				{Path: "main.go", Content: "package main", LineCount: 1},
			},
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_cat",
		Arguments: map[string]any{"repo": mcpTestRepo, "files": []any{"main.go"}, "lines": "1-10"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if len(mock.lastCatReq.Files) != 1 || mock.lastCatReq.Files[0] != "main.go" {
		t.Errorf("files = %v, want [main.go]", mock.lastCatReq.Files)
	}
	if mock.lastCatReq.Lines != "1-10" {
		t.Errorf("lines = %q, want %q", mock.lastCatReq.Lines, "1-10")
	}
}

func TestCatToolNoFiles(t *testing.T) {
	mock := &mockClient{}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_cat",
		Arguments: map[string]any{"repo": mcpTestRepo, "files": []any{}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty files")
	}
}

func TestTreeTool(t *testing.T) {
	mock := &mockClient{
		treeResult: &service.TreeResult{Repo: mcpTestRepo, Files: []string{"internal/a_test.go", "main.go"}},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "cartograph_tree",
		Arguments: map[string]any{"repo": mcpTestRepo},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	if mock.lastTreeReq.Repo != mcpTestRepo {
		t.Errorf("repo = %q, want %q", mock.lastTreeReq.Repo, mcpTestRepo)
	}

	var result service.TreeResult
	if err := json.Unmarshal([]byte(extractText(t, res)), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Repo != mcpTestRepo || strings.Join(result.Files, ",") != "internal/a_test.go,main.go" {
		t.Errorf("unexpected tree: %+v", result)
	}
}

func TestSchemaTool(t *testing.T) {
	mock := &mockClient{
		schemaResult: &service.SchemaResult{
			NodeLabels: []service.NodeLabelSummary{{Label: "Function", Count: 42}},
			RelTypes:   []service.RelTypeSummary{{Type: "CALLS", Count: 100}},
			TotalNodes: 42,
			TotalEdges: 100,
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_schema",
		Arguments: map[string]any{"repo": mcpTestRepo},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	text := extractText(t, res)
	var result service.SchemaResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.TotalNodes != 42 {
		t.Errorf("totalNodes = %d, want 42", result.TotalNodes)
	}
}

func TestHealthTool(t *testing.T) {
	mock := &mockClient{
		healthResult: &service.HealthResult{
			Running: true,
			Ready:   true,
			LoadedRepos: []service.RepoStatus{
				{Name: "cartograph", NodeCount: 1000, EdgeCount: 5000},
			},
			Uptime: "5m0s",
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_health",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	text := extractText(t, res)
	var result service.HealthResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Running {
		t.Error("expected running=true")
	}
	if len(result.LoadedRepos) != 1 {
		t.Errorf("loadedRepos count = %d, want 1", len(result.LoadedRepos))
	}
}

func TestListTool(t *testing.T) {
	mock := &mockClient{
		listResult: &service.ListResult{Repos: []service.RepoInfo{
			{Name: mcpTestRepo, Hash: "h1", Type: "local", NodeCount: 10, EdgeCount: 20, Embedding: "none"},
		}},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "cartograph_list",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	var result service.ListResult
	if err := json.Unmarshal([]byte(extractText(t, res)), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Repos) != 1 || result.Repos[0].Name != mcpTestRepo {
		t.Errorf("unexpected repos: %+v", result.Repos)
	}
}

func TestStatusTool(t *testing.T) {
	mock := &mockClient{
		statusResult: &service.StatusResult{
			Name: mcpTestRepo, Hash: "h1", Type: "local", Indexed: true,
			NodeCount: 100, EdgeCount: 200, EmbeddingStatus: "none",
		},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	res, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "cartograph_status",
		Arguments: map[string]any{"repo": mcpTestRepo},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}
	var result service.StatusResult
	if err := json.Unmarshal([]byte(extractText(t, res)), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Name != mcpTestRepo || !result.Indexed || result.NodeCount != 100 {
		t.Errorf("unexpected status: %+v", result)
	}
	if mock.lastStatusReq.Repo != mcpTestRepo {
		t.Errorf("repo not forwarded: %+v", mock.lastStatusReq)
	}
}

func TestToolError(t *testing.T) {
	mock := &mockClient{
		err: &service.APIError{Code: -32001, Message: "repository \"nope\" not indexed"},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_query",
		Arguments: map[string]any{"repo": "nope", "query": "anything"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true when backend returns error")
	}
}

func TestQueryDefaultLimit(t *testing.T) {
	mock := &mockClient{
		queryResult: &service.QueryResult{},
	}
	session := connectTestServer(t, mock)
	defer session.Close()

	ctx := context.Background()
	_, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_query",
		Arguments: map[string]any{"repo": mcpTestRepo, "query": "test"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if mock.lastQueryReq.Limit != 10 {
		t.Errorf("default limit = %d, want 10", mock.lastQueryReq.Limit)
	}
}

// extractText returns the text content from a CallToolResult.
func extractText(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *TextContent", res.Content[0])
	}
	return tc.Text
}

// TestStreamableHTTPTransport verifies that MCP tools work over the
// Streamable HTTP transport (the same transport used by cartograph serve).
func TestStreamableHTTPTransport(t *testing.T) {
	mock := &mockClient{
		queryResult: &service.QueryResult{
			Processes: []service.ProcessMatch{
				{Name: "handleHTTP", Relevance: 0.9},
			},
		},
		healthResult: &service.HealthResult{Running: true},
	}

	mcpSrv := NewServer("test", mock)
	handler := sdkmcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *sdkmcp.Server {
			return mcpSrv.SDKServer()
		},
		&sdkmcp.StreamableHTTPOptions{Stateless: true},
	)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "test-http-client", Version: "v0.0.1"},
		nil,
	)

	ctx := context.Background()
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint: ts.URL,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) != 11 {
		t.Errorf("tool count = %d, want 11", len(tools.Tools))
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_query",
		Arguments: map[string]any{"repo": mcpTestRepo, "query": "HTTP handler"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	text := extractText(t, res)
	var result service.QueryResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Processes) != 1 || result.Processes[0].Name != "handleHTTP" {
		t.Errorf("unexpected processes: %+v", result.Processes)
	}
}

// TestStreamableHTTPHealth verifies the health tool over HTTP.
func TestStreamableHTTPHealth(t *testing.T) {
	mock := &mockClient{
		healthResult: &service.HealthResult{
			Running: true,
			Ready:   true,
			LoadedRepos: []service.RepoStatus{
				{Name: "testrepo", NodeCount: 100, EdgeCount: 500},
			},
		},
	}

	mcpSrv := NewServer("test", mock)
	handler := sdkmcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *sdkmcp.Server {
			return mcpSrv.SDKServer()
		},
		&sdkmcp.StreamableHTTPOptions{Stateless: true},
	)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := sdkmcp.NewClient(
		&sdkmcp.Implementation{Name: "test-http-client", Version: "v0.0.1"},
		nil,
	)

	ctx := context.Background()
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint: ts.URL,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "cartograph_health",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.Content)
	}

	text := extractText(t, res)
	var result service.HealthResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !result.Running {
		t.Error("expected running=true")
	}
	if len(result.LoadedRepos) != 1 {
		t.Errorf("loadedRepos count = %d, want 1", len(result.LoadedRepos))
	}
}
