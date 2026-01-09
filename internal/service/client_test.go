package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudprivacylabs/lpg/v2"

	"github.com/realxen/cartograph/internal/search"
)

const tcpNetwork = "tcp"

// testClientServer returns a Client pointed at an httptest.Server that
// uses the real Server handler.
func testClientServer(t *testing.T) *Client {
	t.Helper()
	s := &Server{
		graph:       make(map[string]*lpg.Graph),
		searchIdx:   make(map[string]*search.Index),
		idleTimeout: DefaultIdleTimeout,
	}
	s.graph["myrepo"] = lpg.NewGraph()
	s.backendFactory = func(repo string) ToolBackend {
		if _, ok := s.graph[repo]; !ok {
			return nil
		}
		return stubBackend{}
	}

	mux := s.SetupRoutes()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	cl := &Client{
		httpClient: ts.Client(),
	}
	cl.httpClient.Transport = &testTransport{
		base:    ts.Client().Transport,
		baseURL: ts.URL,
	}

	return cl
}

// testTransport rewrites "http://localhost" to the test server URL.
type testTransport struct {
	base    http.RoundTripper
	baseURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.baseURL[len("http://"):]
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("test transport: %w", err)
	}
	return resp, nil
}

func TestClientQuery(t *testing.T) {
	cl := testClientServer(t)
	res, err := cl.Query(QueryRequest{Repo: "myrepo", Text: "test", Limit: 5})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestClientContext(t *testing.T) {
	cl := testClientServer(t)
	res, err := cl.Context(ContextRequest{Repo: "myrepo", Name: "main"})
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestClientCypher(t *testing.T) {
	cl := testClientServer(t)
	res, err := cl.Cypher(CypherRequest{Repo: "myrepo", Query: "MATCH (n) RETURN n"})
	if err != nil {
		t.Fatalf("cypher: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestClientCypherWriteBlocked(t *testing.T) {
	cl := testClientServer(t)
	_, err := cl.Cypher(CypherRequest{Repo: "myrepo", Query: "CREATE (n:Bad)"})
	if err == nil {
		t.Fatal("expected error for write query")
	}
}

func TestClientImpact(t *testing.T) {
	cl := testClientServer(t)
	res, err := cl.Impact(ImpactRequest{Repo: "myrepo", Target: "main", Direction: "downstream", Depth: 2})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestClientReload(t *testing.T) {
	cl := testClientServer(t)
	err := cl.Reload(ReloadRequest{Repo: "myrepo"})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func TestClientStatus(t *testing.T) {
	cl := testClientServer(t)
	res, err := cl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !res.Running {
		t.Error("expected running=true")
	}
}

func TestClientShutdown(t *testing.T) {
	cl := testClientServer(t)
	err := cl.Shutdown()
	if err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestClientErrorPropagation(t *testing.T) {
	cl := testClientServer(t)
	_, err := cl.Query(QueryRequest{Repo: "nonexistent", Text: "x"})
	if err == nil {
		t.Fatal("expected error for nonexistent repo")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != ErrCodeRepoNotFound {
		t.Errorf("expected code %d, got %d", ErrCodeRepoNotFound, apiErr.Code)
	}
}

func TestClientMockHandler(t *testing.T) {
	// Test with a completely custom mock handler.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{ //nolint:errchkjson
			Result: &StatusResult{Running: true, Uptime: "1h0m0s"},
		})
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	cl := &Client{
		httpClient: ts.Client(),
	}
	cl.httpClient.Transport = &testTransport{
		base:    ts.Client().Transport,
		baseURL: ts.URL,
	}

	res, err := cl.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !res.Running {
		t.Error("expected running=true from mock")
	}
}

func TestLooksLikeTCP_TCPAddresses(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:3000", true},
		{"[::1]:8080", true},
		{"/tmp/carto.sock", false},
		{"/var/run/app.socket", false},
		{"C:\\Users\\test\\app.sock", false},
		{"carto.sock", false},
	}
	for _, tc := range cases {
		got := looksLikeTCP(tc.addr)
		if got != tc.want {
			t.Errorf("looksLikeTCP(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestNewAutoClient_TCP(t *testing.T) {
	cl := NewAutoClient("127.0.0.1:8080")
	if cl == nil {
		t.Fatal("expected non-nil client")
	}
	if cl.network != tcpNetwork {
		t.Errorf("expected network 'tcp', got %q", cl.network)
	}
}

func TestNewAutoClient_Unix(t *testing.T) {
	cl := NewAutoClient("/tmp/carto.sock")
	if cl == nil {
		t.Fatal("expected non-nil client")
	}
	if cl.network != networkUnix {
		t.Errorf("expected network %q, got %q", networkUnix, cl.network)
	}
}

func TestNewTCPClient(t *testing.T) {
	cl := NewTCPClient("localhost:9999")
	if cl == nil {
		t.Fatal("expected non-nil client")
	}
	if cl.network != tcpNetwork {
		t.Errorf("expected tcp network, got %q", cl.network)
	}
	if cl.addr != "localhost:9999" {
		t.Errorf("expected addr localhost:9999, got %q", cl.addr)
	}
}
