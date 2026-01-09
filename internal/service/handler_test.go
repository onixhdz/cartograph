package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"
	"github.com/realxen/cartograph/internal/search"
	"github.com/realxen/cartograph/internal/storage"
)

// stubBackend is a minimal ToolBackend for handler tests.
type stubBackend struct{}

func (stubBackend) Query(req QueryRequest) (*QueryResult, error) {
	return &QueryResult{
		Processes:      []ProcessMatch{},
		ProcessSymbols: []SymbolMatch{},
		Definitions:    []SymbolMatch{},
	}, nil
}
func (stubBackend) Context(req ContextRequest) (*ContextResult, error) {
	return &ContextResult{
		Symbol:    SymbolMatch{},
		Callers:   []SymbolMatch{},
		Callees:   []SymbolMatch{},
		Importers: []SymbolMatch{},
		Imports:   []SymbolMatch{},
		Processes: []SymbolMatch{},
	}, nil
}
func (stubBackend) Cypher(req CypherRequest) (*CypherResult, error) {
	return &CypherResult{
		Columns: []string{},
		Rows:    []map[string]any{},
	}, nil
}
func (stubBackend) Impact(req ImpactRequest) (*ImpactResult, error) {
	return &ImpactResult{
		Target:   SymbolMatch{},
		Affected: []SymbolMatch{},
		Depth:    req.Depth,
	}, nil
}
func (stubBackend) Schema(req SchemaRequest) (*SchemaResult, error) {
	return &SchemaResult{
		NodeLabels: []NodeLabelSummary{},
		RelTypes:   []RelTypeSummary{},
		Properties: []string{},
	}, nil
}

// newTestServer returns a Server with an in-memory graph for "testrepo".
func newTestServer() *Server {
	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		idleTimeout: DefaultIdleTimeout,
	}
	s.graph["testrepo"] = lpg.NewGraph()
	s.backendFactory = func(repo string) ToolBackend {
		if _, ok := s.graph[repo]; !ok {
			return nil
		}
		return stubBackend{}
	}
	return s
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewBuffer(b)
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

func TestWriteJSONAndWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, map[string]string{"hello": "world"})
	if rec.Code != http.StatusOK {
		t.Errorf("writeJSON status: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type: %q", ct)
	}

	rec2 := httptest.NewRecorder()
	writeError(rec2, http.StatusBadRequest, "bad")
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("writeError status: %d", rec2.Code)
	}
	resp := decodeResponse(t, rec2)
	if resp.Error == nil {
		t.Fatal("expected error in response")
	}
	if resp.Error.Message != "bad" {
		t.Errorf("error message: %q", resp.Error.Message)
	}
}

func TestDecodeJSONEmpty(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	var v map[string]string
	if err := decodeJSON(req, &v); err == nil {
		t.Error("expected error for empty body")
	}
}

func TestHandleQuery(t *testing.T) {
	s := newTestServer()
	body := jsonBody(t, QueryRequest{Repo: "testrepo", Text: "hello", Limit: 10})
	req := httptest.NewRequest("POST", RouteQuery, body)
	rec := httptest.NewRecorder()
	s.handleQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rec.Code, rec.Body.String())
	}
	resp := decodeResponse(t, rec)
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
}

func TestHandleQueryRepoNotFound(t *testing.T) {
	s := newTestServer()
	body := jsonBody(t, QueryRequest{Repo: "unknown", Text: "x"})
	req := httptest.NewRequest("POST", RouteQuery, body)
	rec := httptest.NewRecorder()
	s.handleQuery(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for unknown repo")
	}
	if resp.Error.Code != ErrCodeRepoNotFound {
		t.Errorf("error code: %d", resp.Error.Code)
	}
}

func TestHandleQueryMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest("GET", RouteQuery, nil)
	rec := httptest.NewRecorder()
	s.handleQuery(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleContext(t *testing.T) {
	s := newTestServer()
	body := jsonBody(t, ContextRequest{Repo: "testrepo", Name: "main"})
	req := httptest.NewRequest("POST", RouteContext, body)
	rec := httptest.NewRecorder()
	s.handleContext(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}

func TestHandleCypher(t *testing.T) {
	s := newTestServer()
	body := jsonBody(t, CypherRequest{Repo: "testrepo", Query: "MATCH (n) RETURN n LIMIT 1"})
	req := httptest.NewRequest("POST", RouteCypher, body)
	rec := httptest.NewRecorder()
	s.handleCypher(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCypherBlocksWrite(t *testing.T) {
	s := newTestServer()
	writeQueries := []string{
		"CREATE (n:Node {name:'bad'})",
		"MATCH (n) DELETE n",
		"MATCH (n) SET n.x = 1",
		"MERGE (n:Node {id:1})",
		"DROP INDEX ON :Node(name)",
		"ALTER TABLE foo",
		"MATCH (n) DETACH DELETE n",
	}
	for _, q := range writeQueries {
		body := jsonBody(t, CypherRequest{Repo: "testrepo", Query: q})
		req := httptest.NewRequest("POST", RouteCypher, body)
		rec := httptest.NewRecorder()
		s.handleCypher(rec, req)

		resp := decodeResponse(t, rec)
		if resp.Error == nil {
			t.Errorf("expected write query blocked for %q", q)
			continue
		}
		if resp.Error.Code != ErrCodeQueryBlocked {
			t.Errorf("query %q: wrong error code %d", q, resp.Error.Code)
		}
	}
}

func TestHandleImpact(t *testing.T) {
	s := newTestServer()
	body := jsonBody(t, ImpactRequest{Repo: "testrepo", Target: "main", Direction: "downstream", Depth: 3})
	req := httptest.NewRequest("POST", RouteImpact, body)
	rec := httptest.NewRecorder()
	s.handleImpact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}

func TestHandleReload(t *testing.T) {
	s := newTestServer()
	body := jsonBody(t, ReloadRequest{Repo: "testrepo"})
	req := httptest.NewRequest("POST", RouteReload, body)
	rec := httptest.NewRecorder()
	s.handleReload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}

	if _, ok := s.GetGraph("testrepo"); ok {
		t.Error("expected graph to be evicted after reload")
	}
}

func TestHandleStatus(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest("GET", RouteStatus, nil)
	rec := httptest.NewRecorder()
	s.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	resp := decodeResponse(t, rec)
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
}

func TestHandleShutdown(t *testing.T) {
	s := newTestServer()
	s.done = make(chan struct{})
	req := httptest.NewRequest("POST", RouteShutdown, nil)
	rec := httptest.NewRecorder()
	s.handleShutdown(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}

func TestUnknownRoute404(t *testing.T) {
	s := newTestServer()
	handler := s.SetupRoutes()
	req := httptest.NewRequest("GET", "/api/nonexistent", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", rec.Code)
	}
}

func TestIsCypherWriteQuery(t *testing.T) {
	if !isCypherWriteQuery("CREATE (n:Node)") {
		t.Error("expected true for CREATE")
	}
	if isCypherWriteQuery("MATCH (n) RETURN n") {
		t.Error("expected false for read-only query")
	}
}

func TestDecodeJSON_BodyTooLarge(t *testing.T) {
	big := make([]byte, maxRequestBody+1024)
	for i := range big {
		big[i] = 'x'
	}
	req := httptest.NewRequest("POST", "/", bytes.NewReader(big))
	var v map[string]string
	err := decodeJSON(req, &v)
	if err == nil {
		t.Fatal("expected error for body exceeding limit")
	}
}

func TestDecodeJSON_WithinLimit(t *testing.T) {
	payload := `{"repo":"test","text":"hello"}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(payload))
	var v QueryRequest
	if err := decodeJSON(req, &v); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Repo != "test" {
		t.Errorf("repo: got %q, want %q", v.Repo, "test")
	}
}

func TestRecoveryMiddleware_CatchesPanic(t *testing.T) {
	panicker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	handler := recoveryMiddleware(panicker)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("internal server error")) {
		t.Errorf("expected error body, got %q", rec.Body.String())
	}
}

func TestRecoveryMiddleware_PassesThrough(t *testing.T) {
	normal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok")) //nolint:errcheck
	})
	handler := recoveryMiddleware(normal)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// newTestServerWithRegistry returns a Server backed by a temp-dir registry
// containing two repos that share the short name "sdk".
func newTestServerWithRegistry(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	reg, err := storage.NewRegistry(dir)
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	_ = reg.Add(storage.RegistryEntry{Name: "acme/sdk", Hash: "h1"})
	_ = reg.Add(storage.RegistryEntry{Name: "corp/sdk", Hash: "h2"})

	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		resolvers:   make(map[string]*storage.ContentResolver),
		repoDirs:    make(map[string]string),
		embedJobs:   make(map[string]*embedJob),
		embedSem:    make(chan struct{}, 1),
		dataDir:     dir,
		idleTimeout: DefaultIdleTimeout,
	}
	s.graph["acme/sdk"] = lpg.NewGraph()
	s.backendFactory = func(repo string) ToolBackend {
		if _, ok := s.graph[repo]; !ok {
			return nil
		}
		return stubBackend{}
	}
	return s
}

func TestHandleQuery_AmbiguousShortName(t *testing.T) {
	s := newTestServerWithRegistry(t)
	body := jsonBody(t, QueryRequest{Repo: "sdk", Text: "test"})
	req := httptest.NewRequest("POST", RouteQuery, body)
	rec := httptest.NewRecorder()
	s.handleQuery(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if resp.Error.Code != ErrCodeRepoNotFound {
		t.Errorf("error code: want %d, got %d", ErrCodeRepoNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "ambiguous") {
		t.Errorf("error should mention 'ambiguous', got: %s", resp.Error.Message)
	}
	if !strings.Contains(resp.Error.Message, "acme/sdk") || !strings.Contains(resp.Error.Message, "corp/sdk") {
		t.Errorf("error should list both repos, got: %s", resp.Error.Message)
	}
}

func TestHandleContext_AmbiguousShortName(t *testing.T) {
	s := newTestServerWithRegistry(t)
	body := jsonBody(t, ContextRequest{Repo: "sdk", Name: "main"})
	req := httptest.NewRequest("POST", RouteContext, body)
	rec := httptest.NewRecorder()
	s.handleContext(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if !strings.Contains(resp.Error.Message, "ambiguous") {
		t.Errorf("error should mention 'ambiguous', got: %s", resp.Error.Message)
	}
}

func TestHandleCypher_AmbiguousShortName(t *testing.T) {
	s := newTestServerWithRegistry(t)
	body := jsonBody(t, CypherRequest{Repo: "sdk", Query: "MATCH (n) RETURN n"})
	req := httptest.NewRequest("POST", RouteCypher, body)
	rec := httptest.NewRecorder()
	s.handleCypher(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if !strings.Contains(resp.Error.Message, "ambiguous") {
		t.Errorf("error should mention 'ambiguous', got: %s", resp.Error.Message)
	}
}

func TestHandleImpact_AmbiguousShortName(t *testing.T) {
	s := newTestServerWithRegistry(t)
	body := jsonBody(t, ImpactRequest{Repo: "sdk", Target: "main", Depth: 3})
	req := httptest.NewRequest("POST", RouteImpact, body)
	rec := httptest.NewRecorder()
	s.handleImpact(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if !strings.Contains(resp.Error.Message, "ambiguous") {
		t.Errorf("error should mention 'ambiguous', got: %s", resp.Error.Message)
	}
}

func TestHandleSchema_AmbiguousShortName(t *testing.T) {
	s := newTestServerWithRegistry(t)
	body := jsonBody(t, SchemaRequest{Repo: "sdk"})
	req := httptest.NewRequest("POST", RouteSchema, body)
	rec := httptest.NewRecorder()
	s.handleSchema(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if !strings.Contains(resp.Error.Message, "ambiguous") {
		t.Errorf("error should mention 'ambiguous', got: %s", resp.Error.Message)
	}
}

func TestHandleReload_AmbiguousShortName(t *testing.T) {
	s := newTestServerWithRegistry(t)
	body := jsonBody(t, ReloadRequest{Repo: "sdk"})
	req := httptest.NewRequest("POST", RouteReload, body)
	rec := httptest.NewRecorder()
	s.handleReload(rec, req)

	resp := decodeResponse(t, rec)
	if resp.Error == nil {
		t.Fatal("expected error for ambiguous short name")
	}
	if !strings.Contains(resp.Error.Message, "ambiguous") {
		t.Errorf("error should mention 'ambiguous', got: %s", resp.Error.Message)
	}
}

func TestHandleQuery_ShortNameResolvesViaRegistry(t *testing.T) {
	dir := t.TempDir()
	reg, _ := storage.NewRegistry(dir)
	_ = reg.Add(storage.RegistryEntry{Name: "acme/sdk", Hash: "h1"})

	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		resolvers:   make(map[string]*storage.ContentResolver),
		repoDirs:    make(map[string]string),
		embedJobs:   make(map[string]*embedJob),
		embedSem:    make(chan struct{}, 1),
		dataDir:     dir,
		idleTimeout: DefaultIdleTimeout,
	}
	s.graph["acme/sdk"] = lpg.NewGraph()
	s.backendFactory = func(repo string) ToolBackend {
		if _, ok := s.graph[repo]; !ok {
			return nil
		}
		return stubBackend{}
	}

	body := jsonBody(t, QueryRequest{Repo: "sdk", Text: "test", Limit: 5})
	req := httptest.NewRequest("POST", RouteQuery, body)
	rec := httptest.NewRecorder()
	s.handleQuery(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeResponse(t, rec)
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
}
