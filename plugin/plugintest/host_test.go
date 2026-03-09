package plugintest

import (
	"context"
	"fmt"
	"testing"

	"github.com/realxen/cartograph/plugin"
)

func TestHost_ConfigGet(t *testing.T) {
	h := NewHost(Config{"token": "abc123", "org": "acme"})
	ctx := context.Background()

	t.Run("existing key", func(t *testing.T) {
		val, err := h.ConfigGet(ctx, "token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != "abc123" {
			t.Errorf("got %q, want %q", val, "abc123")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, err := h.ConfigGet(ctx, "nonexistent")
		if err == nil {
			t.Fatal("expected error for missing key")
		}
	})
}

func TestHost_Cache(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()

	t.Run("miss", func(t *testing.T) {
		_, found, err := h.CacheGet(ctx, "k")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Error("expected not found")
		}
	})

	t.Run("set and get", func(t *testing.T) {
		if err := h.CacheSet(ctx, "k", "v", 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		val, found, err := h.CacheGet(ctx, "k")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found")
		}
		if val != "v" {
			t.Errorf("got %q, want %q", val, "v")
		}
	})
}

func TestHost_Emissions(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()

	if err := h.EmitNode(ctx, "MyWidget", "w:1", map[string]any{"name": "Sprocket"}); err != nil {
		t.Fatal(err)
	}
	if err := h.EmitNode(ctx, "MyOwner", "o:alice", map[string]any{"login": "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := h.EmitEdge(ctx, "o:alice", "w:1", "OWNS", nil); err != nil {
		t.Fatal(err)
	}

	h.AssertNodeCount(t, 2)
	h.AssertEdgeCount(t, 1)
	h.AssertNodeExists(t, "w:1", "MyWidget")
	h.AssertNodeExists(t, "o:alice", "MyOwner")
	h.AssertEdgeExists(t, "o:alice", "w:1", "OWNS")

	nodes := h.Nodes()
	if len(nodes) != 2 {
		t.Fatalf("Nodes() returned %d, want 2", len(nodes))
	}
	if nodes[0].Props["name"] != "Sprocket" {
		t.Errorf("first node name: got %v, want Sprocket", nodes[0].Props["name"])
	}
}

func TestHost_Log(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()

	_ = h.Log(ctx, "info", "fetched 42 repos")
	_ = h.Log(ctx, "warn", "rate limit approaching")

	h.AssertLogContains(t, "info", "42 repos")
	h.AssertLogContains(t, "warn", "rate limit")

	logs := h.Logs()
	if len(logs) != 2 {
		t.Fatalf("Logs() returned %d, want 2", len(logs))
	}
}

func TestHost_HTTPRequest_NoHandler(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()

	_, err := h.HTTPRequest(ctx, plugin.HTTPRequest{Method: "GET", URL: "http://example.com"})
	if err == nil {
		t.Fatal("expected error when no HTTP handler configured")
	}
}

func TestHost_HTTPRequest_WithHandler(t *testing.T) {
	h := NewHost(nil)
	ctx := context.Background()

	h.SetHTTPHandler(func(_ context.Context, req plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
		return &plugin.HTTPResponse{Status: 200, Body: `{"ok":true}`}, nil
	})

	resp, err := h.HTTPRequest(ctx, plugin.HTTPRequest{Method: "GET", URL: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("status: got %d, want 200", resp.Status)
	}
}

// TestHost_PluginLifecycle exercises a complete plugin lifecycle using the mock host.
func TestHost_PluginLifecycle(t *testing.T) {
	h := NewHost(Config{"api_key": "test-key"})
	ctx := context.Background()

	p := &fakePlugin{}

	if err := p.Configure(ctx, h, "my_conn"); err != nil {
		t.Fatal(err)
	}
	if p.key != "test-key" {
		t.Errorf("plugin key: got %q, want %q", p.key, "test-key")
	}

	result, err := p.Ingest(ctx, h, plugin.IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Nodes != 2 {
		t.Errorf("result.Nodes: got %d, want 2", result.Nodes)
	}
	if result.Edges != 1 {
		t.Errorf("result.Edges: got %d, want 1", result.Edges)
	}

	h.AssertNodeCount(t, 2)
	h.AssertEdgeCount(t, 1)
	h.AssertNodeExists(t, "fake:widget:1", "FakeWidget")
	h.AssertNodeExists(t, "fake:owner:alice", "FakeOwner")
	h.AssertEdgeExists(t, "fake:owner:alice", "fake:widget:1", "OWNS")
	h.AssertLogContains(t, "info", "done")
}

// fakePlugin is a minimal plugin for testing the mock host.
type fakePlugin struct {
	key string
}

func (p *fakePlugin) Info() plugin.Info {
	return plugin.Info{
		Name:    "fake",
		Version: "0.1.0",
		Resources: []plugin.Resource{
			{Name: "Widget", Label: "FakeWidget"},
			{Name: "Owner", Label: "FakeOwner"},
		},
	}
}

func (p *fakePlugin) Configure(ctx context.Context, host plugin.Host, _ string) error {
	key, err := host.ConfigGet(ctx, "api_key")
	if err != nil {
		return fmt.Errorf("config_get: %w", err)
	}
	p.key = key
	return nil
}

func (p *fakePlugin) Ingest(ctx context.Context, host plugin.Host, _ plugin.IngestOptions) (plugin.IngestResult, error) {
	if err := host.EmitNode(ctx, "FakeWidget", "fake:widget:1", map[string]any{"name": "Sprocket"}); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit: %w", err)
	}
	if err := host.EmitNode(ctx, "FakeOwner", "fake:owner:alice", map[string]any{"login": "alice"}); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit: %w", err)
	}
	if err := host.EmitEdge(ctx, "fake:owner:alice", "fake:widget:1", "OWNS", nil); err != nil {
		return plugin.IngestResult{}, fmt.Errorf("emit: %w", err)
	}
	_ = host.Log(ctx, "info", "done")
	return plugin.IngestResult{Nodes: 2, Edges: 1}, nil
}
