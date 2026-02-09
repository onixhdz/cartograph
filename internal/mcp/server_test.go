package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/realxen/cartograph/internal/service"
)

// mockClient implements the Client interface with canned responses
// for testing the MCP tool handlers without needing a real graph.
type mockClient struct {
	queryResult   *service.QueryResult
	contextResult *service.ContextResult
	impactResult  *service.ImpactResult
	cypherResult  *service.CypherResult
	catResult     *service.CatResult
	schemaResult  *service.SchemaResult
	statusResult  *service.StatusResult
	err           error

	// capture last request for assertions
	lastQueryReq   service.QueryRequest
	lastContextReq service.ContextRequest
	lastImpactReq  service.ImpactRequest
	lastCypherReq  service.CypherRequest
	lastCatReq     service.CatRequest
	lastSchemaReq  service.SchemaRequest
}

func (m *mockClient) Query(req service.QueryRequest) (*service.QueryResult, error) {
	m.lastQueryReq = req
	return m.queryResult, m.err
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

func (m *mockClient) Schema(req service.SchemaRequest) (*service.SchemaResult, error) {
	m.lastSchemaReq = req
	return m.schemaResult, m.err
}

func (m *mockClient) Status() (*service.StatusResult, error) {
	return m.statusResult, m.err
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
		statusResult: &service.StatusResult{Running: true},
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
		"cartograph_context": false,
		"cartograph_impact":  false,
		"cartograph_cypher":  false,
		"cartograph_cat":     false,
		"cartograph_schema":  false,
		"cartograph_status":  false,
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
		Arguments: map[string]any{"repo": "myrepo", "query": "HTTP handler", "limit": float64(5)},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastQueryReq.Repo != "myrepo" {
		t.Errorf("repo = %q, want %q", mock.lastQueryReq.Repo, "myrepo")
	}
	if mock.lastQueryReq.Text != "HTTP handler" {
		t.Errorf("text = %q, want %q", mock.lastQueryReq.Text, "HTTP handler")
	}
	if mock.lastQueryReq.Limit != 5 {
		t.Errorf("limit = %d, want %d", mock.lastQueryReq.Limit, 5)
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
		Arguments: map[string]any{"repo": "myrepo", "symbol": "Serve"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	if mock.lastContextReq.Repo != "myrepo" {
		t.Errorf("repo = %q, want %q", mock.lastContextReq.Repo, "myrepo")
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
		Arguments: map[string]any{"repo": "myrepo", "target": "Connect"},
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
		Arguments: map[string]any{"repo": "myrepo", "query": "MATCH (n:Function) RETURN n.name LIMIT 1"},
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
		Arguments: map[string]any{"repo": "myrepo", "files": []any{"main.go"}, "lines": "1-10"},
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
		Arguments: map[string]any{"repo": "myrepo", "files": []any{}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected IsError=true for empty files")
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
		Arguments: map[string]any{"repo": "myrepo"},
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

func TestStatusTool(t *testing.T) {
	mock := &mockClient{
		statusResult: &service.StatusResult{
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
		Name:      "cartograph_status",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	text := extractText(t, res)
	var result service.StatusResult
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
		Arguments: map[string]any{"repo": "myrepo", "query": "test"},
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
