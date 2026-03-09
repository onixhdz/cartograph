// Package plugintest provides test utilities for Cartograph plugin authors.
//
// The primary type is [Host], a mock implementation of [plugin.Host] that
// records emitted nodes, edges, and log messages for assertion in tests.
// No Cartograph installation or running host process is required.
//
// For plugins that use host.HTTPRequest, [MockHTTP] builds a handler from
// a route table of canned responses.
//
// For full integration testing against a compiled plugin binary,
// [RunBinary] launches the binary as a subprocess and runs the complete
// protocol lifecycle (handshake → info → configure → ingest → close).
package plugintest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/realxen/cartograph/plugin"
)

// Compile-time check.
var _ plugin.Host = (*Host)(nil)

// Config is a convenience alias for plugin configuration maps.
type Config map[string]string

// Node is a recorded node emission.
type Node struct {
	Label string
	ID    string
	Props map[string]any
}

// Edge is a recorded edge emission.
type Edge struct {
	FromID  string
	ToID    string
	RelType string
	Props   map[string]any
}

// LogEntry is a recorded log message.
type LogEntry struct {
	Level string
	Msg   string
}

// Host is a mock plugin.Host that records all emissions and log messages.
// All methods are safe for concurrent use.
type Host struct {
	mu     sync.Mutex
	config Config
	cache  map[string]cacheEntry
	httpFn func(ctx context.Context, req plugin.HTTPRequest) (*plugin.HTTPResponse, error)

	nodes []Node
	edges []Edge
	logs  []LogEntry
}

type cacheEntry struct {
	value   string
	expires time.Time
}

// NewHost creates a mock Host with the given config values.
func NewHost(config Config) *Host {
	if config == nil {
		config = Config{}
	}
	return &Host{
		config: config,
		cache:  make(map[string]cacheEntry),
	}
}

// SetHTTPHandler sets the function used to handle HTTPRequest calls.
// Use [MockHTTP] to build one from a route table.
func (h *Host) SetHTTPHandler(fn func(ctx context.Context, req plugin.HTTPRequest) (*plugin.HTTPResponse, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.httpFn = fn
}

// ConfigGet returns the config value for key, or an error if not found.
func (h *Host) ConfigGet(_ context.Context, key string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	val, ok := h.config[key]
	if !ok {
		return "", fmt.Errorf("config key %q not found", key)
	}
	return val, nil
}

// CacheGet returns the cached value for key. Expired entries are not returned.
func (h *Host) CacheGet(_ context.Context, key string) (string, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry, ok := h.cache[key]
	if !ok {
		return "", false, nil
	}
	if !entry.expires.IsZero() && time.Now().After(entry.expires) {
		delete(h.cache, key)
		return "", false, nil
	}
	return entry.value, true, nil
}

// CacheSet stores a value with a TTL in seconds. Zero means no expiry.
func (h *Host) CacheSet(_ context.Context, key, value string, ttlSeconds int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := cacheEntry{value: value}
	if ttlSeconds > 0 {
		entry.expires = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	h.cache[key] = entry
	return nil
}

// HTTPRequest delegates to the configured HTTP handler, or returns an error
// if none is set.
func (h *Host) HTTPRequest(ctx context.Context, req plugin.HTTPRequest) (*plugin.HTTPResponse, error) {
	h.mu.Lock()
	fn := h.httpFn
	h.mu.Unlock()
	if fn == nil {
		return nil, errors.New("plugintest: no HTTP handler configured (use Host.SetHTTPHandler or MockHTTP)")
	}
	return fn(ctx, req)
}

// EmitNode records a node emission.
func (h *Host) EmitNode(_ context.Context, label, id string, props map[string]any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodes = append(h.nodes, Node{Label: label, ID: id, Props: props})
	return nil
}

// EmitEdge records an edge emission.
func (h *Host) EmitEdge(_ context.Context, fromID, toID, relType string, props map[string]any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.edges = append(h.edges, Edge{FromID: fromID, ToID: toID, RelType: relType, Props: props})
	return nil
}

// Log records a log message.
func (h *Host) Log(_ context.Context, level, msg string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, LogEntry{Level: level, Msg: msg})
	return nil
}

// Nodes returns a copy of all emitted nodes.
func (h *Host) Nodes() []Node {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Node, len(h.nodes))
	copy(out, h.nodes)
	return out
}

// Edges returns a copy of all emitted edges.
func (h *Host) Edges() []Edge {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Edge, len(h.edges))
	copy(out, h.edges)
	return out
}

// Logs returns a copy of all recorded log entries.
func (h *Host) Logs() []LogEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]LogEntry, len(h.logs))
	copy(out, h.logs)
	return out
}

// AssertNodeCount fails the test if the number of emitted nodes doesn't match.
func (h *Host) AssertNodeCount(t testing.TB, expected int) {
	t.Helper()
	h.mu.Lock()
	got := len(h.nodes)
	h.mu.Unlock()
	if got != expected {
		t.Errorf("node count: got %d, want %d", got, expected)
	}
}

// AssertEdgeCount fails the test if the number of emitted edges doesn't match.
func (h *Host) AssertEdgeCount(t testing.TB, expected int) {
	t.Helper()
	h.mu.Lock()
	got := len(h.edges)
	h.mu.Unlock()
	if got != expected {
		t.Errorf("edge count: got %d, want %d", got, expected)
	}
}

// AssertNodeExists fails the test if no emitted node matches the given ID and label.
func (h *Host) AssertNodeExists(t testing.TB, id, label string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.nodes {
		if n.ID == id && n.Label == label {
			return
		}
	}
	t.Errorf("node not found: id=%q label=%q", id, label)
}

// AssertEdgeExists fails the test if no emitted edge matches from→to with the given relType.
func (h *Host) AssertEdgeExists(t testing.TB, fromID, toID, relType string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.edges {
		if e.FromID == fromID && e.ToID == toID && e.RelType == relType {
			return
		}
	}
	t.Errorf("edge not found: %q -[%s]-> %q", fromID, relType, toID)
}

// AssertLogContains fails the test if no log entry at the given level contains substr.
func (h *Host) AssertLogContains(t testing.TB, level, substr string) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.logs {
		if l.Level == level && contains(l.Msg, substr) {
			return
		}
	}
	t.Errorf("no %s log containing %q", level, substr)
}

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
