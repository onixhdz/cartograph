package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/realxen/cartograph/internal/jsonrpc2"
	"github.com/realxen/cartograph/internal/plugin"
)

// mockBuilder records AddNode/AddEdge calls for verification.
type mockBuilder struct {
	mu    sync.Mutex
	nodes []mockNode
	edges []mockEdge
}

type mockNode struct {
	label string
	id    string
	props map[string]any
}

type mockEdge struct {
	from    string
	to      string
	relType string
	props   map[string]any
}

func (b *mockBuilder) AddNode(label string, id string, props map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nodes = append(b.nodes, mockNode{label: label, id: id, props: props})
}

func (b *mockBuilder) AddEdge(from string, to string, relType string, props map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.edges = append(b.edges, mockEdge{from: from, to: to, relType: relType, props: props})
}

// makeRequest creates a test JSON-RPC Request with the given method and params.
func makeRequest(method string, params any) *jsonrpc2.Request {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			panic("makeRequest: " + err.Error())
		}
		raw = data
	}
	return &jsonrpc2.Request{
		ID:     jsonrpc2.Int64ID(1),
		Method: method,
		Params: raw,
	}
}

func makeNotification(method string, params any) *jsonrpc2.Request {
	var raw json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			panic("makeNotification: " + err.Error())
		}
		raw = data
	}
	return &jsonrpc2.Request{
		Method: method,
		Params: raw,
	}
}

// --- config_get tests ---

func TestHostHandler_ConfigGet(t *testing.T) {
	h := &plugin.HostHandler{
		Config: map[string]any{
			"token": "ghp_secret",
			"org":   "acme",
			"count": float64(42),
		},
	}
	ctx := context.Background()

	tests := []struct {
		name    string
		key     string
		want    any
		wantErr bool
	}{
		{name: "string value", key: "token", want: "ghp_secret"},
		{name: "another string", key: "org", want: "acme"},
		{name: "numeric value", key: "count", want: float64(42)},
		{name: "missing key", key: "nonexistent", wantErr: true},
		{name: "empty key", key: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeRequest("config_get", map[string]string{"key": tt.key})
			result, err := h.Handle(ctx, req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got result: %v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Marshal+unmarshal to normalize types for comparison.
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			wantData, err := json.Marshal(tt.want)
			if err != nil {
				t.Fatalf("marshal want: %v", err)
			}
			if string(data) != string(wantData) {
				t.Errorf("got %s, want %s", data, wantData)
			}
		})
	}
}

func TestHostHandler_ConfigGet_NilConfig(t *testing.T) {
	h := &plugin.HostHandler{}
	ctx := context.Background()

	req := makeRequest("config_get", map[string]string{"key": "anything"})
	_, err := h.Handle(ctx, req)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

// --- cache_get / cache_set tests ---

func TestHostHandler_Cache(t *testing.T) {
	cache := plugin.NewMemoryCache()
	h := &plugin.HostHandler{Cache: cache}
	ctx := context.Background()

	// Get from empty cache.
	req := makeRequest("cache_get", map[string]string{"key": "etag"})
	result, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var getResult struct {
		Value string `json:"value"`
		Found bool   `json:"found"`
	}
	if err := json.Unmarshal(data, &getResult); err != nil {
		t.Fatal(err)
	}
	if getResult.Found {
		t.Error("expected not found in empty cache")
	}

	// Set a value.
	req = makeRequest("cache_set", map[string]any{
		"key":   "etag",
		"value": "W/\"abc\"",
		"ttl":   300,
	})
	_, err = h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// Get the value back.
	req = makeRequest("cache_get", map[string]string{"key": "etag"})
	result, err = h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &getResult); err != nil {
		t.Fatal(err)
	}
	if !getResult.Found {
		t.Error("expected to find cached value")
	}
	if getResult.Value != "W/\"abc\"" {
		t.Errorf("value = %q, want %q", getResult.Value, "W/\"abc\"")
	}
}

func TestHostHandler_CacheNil(t *testing.T) {
	h := &plugin.HostHandler{} // No cache
	ctx := context.Background()

	// cache_get with nil cache should return not found.
	req := makeRequest("cache_get", map[string]string{"key": "x"})
	result, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var getResult struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal(data, &getResult); err != nil {
		t.Fatal(err)
	}
	if getResult.Found {
		t.Error("expected not found with nil cache")
	}

	// cache_set with nil cache should succeed (no-op).
	req = makeRequest("cache_set", map[string]any{"key": "x", "value": "y", "ttl": 60})
	_, err = h.Handle(ctx, req)
	if err != nil {
		t.Fatalf("cache_set with nil cache should not error: %v", err)
	}
}

func TestHostHandler_CacheEmptyKey(t *testing.T) {
	h := &plugin.HostHandler{Cache: plugin.NewMemoryCache()}
	ctx := context.Background()

	req := makeRequest("cache_get", map[string]string{"key": ""})
	_, err := h.Handle(ctx, req)
	if err == nil {
		t.Fatal("expected error for empty key")
	}

	req = makeRequest("cache_set", map[string]any{"key": "", "value": "x"})
	_, err = h.Handle(ctx, req)
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

// --- http_request tests ---

func TestHostHandler_HTTPRequest(t *testing.T) {
	// Start a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test-header")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"repos": []}`)
	}))
	defer server.Close()

	h := &plugin.HostHandler{
		HTTPClient: server.Client(),
	}
	ctx := context.Background()

	req := makeRequest("http_request", map[string]any{
		"method":  "GET",
		"url":     server.URL + "/api/repos",
		"headers": map[string]string{"Accept": "application/json"},
	})
	result, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var httpResult struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(data, &httpResult); err != nil {
		t.Fatal(err)
	}
	if httpResult.Status != 200 {
		t.Errorf("status = %d, want 200", httpResult.Status)
	}
	if httpResult.Body != `{"repos": []}` {
		t.Errorf("body = %q", httpResult.Body)
	}
	if httpResult.Headers["X-Custom"] != "test-header" {
		t.Errorf("missing custom header")
	}
}

func TestHostHandler_HTTPRequest_InvalidParams(t *testing.T) {
	h := &plugin.HostHandler{}
	ctx := context.Background()

	tests := []struct {
		name   string
		params any
	}{
		{name: "empty method", params: map[string]string{"method": "", "url": "http://example.com"}},
		{name: "empty url", params: map[string]string{"method": "GET", "url": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeRequest("http_request", tt.params)
			_, err := h.Handle(ctx, req)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// --- emit_node tests ---

func TestHostHandler_EmitNode(t *testing.T) {
	builder := &mockBuilder{}
	var nodeCount atomic.Int32
	h := &plugin.HostHandler{
		Builder: builder,
		OnEmitNode: func() {
			nodeCount.Add(1)
		},
	}
	ctx := context.Background()

	req := makeNotification("emit_node", map[string]any{
		"label": "GitHubRepo",
		"id":    "github:repo:acme/api",
		"props": map[string]any{"name": "api", "stars": float64(42)},
	})
	_, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if len(builder.nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(builder.nodes))
	}
	n := builder.nodes[0]
	if n.label != "GitHubRepo" {
		t.Errorf("label = %q", n.label)
	}
	if n.id != "github:repo:acme/api" {
		t.Errorf("id = %q", n.id)
	}
	if n.props["name"] != "api" {
		t.Errorf("props[name] = %v", n.props["name"])
	}
	if nodeCount.Load() != 1 {
		t.Errorf("OnEmitNode called %d times", nodeCount.Load())
	}
}

func TestHostHandler_EmitNode_InvalidParams(t *testing.T) {
	h := &plugin.HostHandler{Builder: &mockBuilder{}}
	ctx := context.Background()

	tests := []struct {
		name   string
		params any
	}{
		{name: "empty label", params: map[string]any{"label": "", "id": "x"}},
		{name: "empty id", params: map[string]any{"label": "X", "id": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeNotification("emit_node", tt.params)
			_, err := h.Handle(ctx, req)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// --- emit_edge tests ---

func TestHostHandler_EmitEdge(t *testing.T) {
	builder := &mockBuilder{}
	var edgeCount atomic.Int32
	h := &plugin.HostHandler{
		Builder: builder,
		OnEmitEdge: func() {
			edgeCount.Add(1)
		},
	}
	ctx := context.Background()

	req := makeNotification("emit_edge", map[string]any{
		"from":  "github:user:alice",
		"to":    "github:repo:acme/api",
		"rel":   "OWNS",
		"props": map[string]any{"since": "2024-01-01"},
	})
	_, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if len(builder.edges) != 1 {
		t.Fatalf("edge count = %d, want 1", len(builder.edges))
	}
	e := builder.edges[0]
	if e.from != "github:user:alice" {
		t.Errorf("from = %q", e.from)
	}
	if e.to != "github:repo:acme/api" {
		t.Errorf("to = %q", e.to)
	}
	if e.relType != "OWNS" {
		t.Errorf("rel = %q", e.relType)
	}
	if edgeCount.Load() != 1 {
		t.Errorf("OnEmitEdge called %d times", edgeCount.Load())
	}
}

func TestHostHandler_EmitEdge_InvalidParams(t *testing.T) {
	h := &plugin.HostHandler{Builder: &mockBuilder{}}
	ctx := context.Background()

	tests := []struct {
		name   string
		params any
	}{
		{name: "empty from", params: map[string]any{"from": "", "to": "x", "rel": "R"}},
		{name: "empty to", params: map[string]any{"from": "x", "to": "", "rel": "R"}},
		{name: "empty rel", params: map[string]any{"from": "x", "to": "y", "rel": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeNotification("emit_edge", tt.params)
			_, err := h.Handle(ctx, req)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// --- log tests ---

func TestHostHandler_Log(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	h := &plugin.HostHandler{
		PluginName: "github",
		Logger: func(name, level, msg string) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, name+"|"+level+"|"+msg)
		},
	}
	ctx := context.Background()

	req := makeNotification("log", map[string]string{
		"level": "info",
		"msg":   "Fetched 42 repos",
	})
	_, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 {
		t.Fatalf("log count = %d, want 1", len(logs))
	}
	if logs[0] != "github|info|Fetched 42 repos" {
		t.Errorf("log = %q", logs[0])
	}
}

func TestHostHandler_LogNilLogger(t *testing.T) {
	h := &plugin.HostHandler{} // No logger
	ctx := context.Background()

	req := makeNotification("log", map[string]string{"level": "warn", "msg": "test"})
	_, err := h.Handle(ctx, req)
	if err != nil {
		t.Fatalf("log with nil logger should not error: %v", err)
	}
}

// --- unknown method ---

func TestHostHandler_UnknownMethod(t *testing.T) {
	h := &plugin.HostHandler{}
	ctx := context.Background()

	req := makeRequest("nonexistent", nil)
	_, err := h.Handle(ctx, req)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
}

// --- MemoryCache tests ---

func TestMemoryCache_TTLExpiry(t *testing.T) {
	cache := plugin.NewMemoryCache()

	// Set with very short TTL.
	cache.Set("key1", "val1", 50*time.Millisecond)
	cache.Set("key2", "val2", 0) // no expiry

	// Immediately should find both.
	if v, ok := cache.Get("key1"); !ok || v != "val1" {
		t.Errorf("key1: got %q/%v", v, ok)
	}
	if v, ok := cache.Get("key2"); !ok || v != "val2" {
		t.Errorf("key2: got %q/%v", v, ok)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	if _, ok := cache.Get("key1"); ok {
		t.Error("key1 should have expired")
	}
	if v, ok := cache.Get("key2"); !ok || v != "val2" {
		t.Error("key2 should still be present (no TTL)")
	}
}

func TestMemoryCache_Overwrite(t *testing.T) {
	cache := plugin.NewMemoryCache()
	cache.Set("k", "v1", 0)
	cache.Set("k", "v2", 0)

	v, ok := cache.Get("k")
	if !ok || v != "v2" {
		t.Errorf("got %q/%v, want v2/true", v, ok)
	}
}

// --- E2E test: full plugin lifecycle with host handlers ---

func TestHostHandler_E2E_FullLifecycle(t *testing.T) {
	bin := buildHostPlugin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	builder := &mockBuilder{}
	var mu sync.Mutex
	var logs []string

	handler := &plugin.HostHandler{
		Config: map[string]any{
			"token": "test-secret-token",
		},
		Builder:    builder,
		Cache:      plugin.NewMemoryCache(),
		PluginName: "testhost",
		Logger: func(name, level, msg string) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, msg)
		},
	}

	p, err := plugin.LaunchPlugin(ctx, bin, plugin.LaunchOptions{
		Handler: handler,
	})
	if err != nil {
		t.Fatalf("LaunchPlugin: %v", err)
	}
	defer p.Close()

	// 1. Call info.
	var info map[string]any
	if err := p.Conn.Call(ctx, "info", nil).Await(ctx, &info); err != nil {
		t.Fatalf("info: %v", err)
	}
	if info["name"] != "testhost" {
		t.Errorf("info name = %v", info["name"])
	}

	// 2. Call configure (plugin calls config_get back to us).
	var configResult map[string]any
	if err := p.Conn.Call(ctx, "configure", map[string]string{"connection": "test"}).Await(ctx, &configResult); err != nil {
		t.Fatalf("configure: %v", err)
	}

	// 3. Call ingest (plugin emits nodes/edges via notifications).
	var ingestResult map[string]any
	if err := p.Conn.Call(ctx, "ingest", nil).Await(ctx, &ingestResult); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Give notifications a moment to be processed.
	time.Sleep(100 * time.Millisecond)

	// Verify emitted nodes.
	builder.mu.Lock()
	nodeCount := len(builder.nodes)
	edgeCount := len(builder.edges)
	builder.mu.Unlock()

	if nodeCount != 2 {
		t.Errorf("nodes = %d, want 2", nodeCount)
	}
	if edgeCount != 1 {
		t.Errorf("edges = %d, want 1", edgeCount)
	}

	// Verify log messages.
	mu.Lock()
	logCount := len(logs)
	mu.Unlock()
	if logCount < 1 {
		t.Errorf("expected at least 1 log message, got %d", logCount)
	}

	// 4. Call close.
	var closeResult map[string]any
	if err := p.Conn.Call(ctx, "close", nil).Await(ctx, &closeResult); err != nil {
		t.Fatalf("close: %v", err)
	}
}
