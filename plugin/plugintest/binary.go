package plugintest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/realxen/cartograph/internal/cloudgraph"
	"github.com/realxen/cartograph/internal/datasource"
	internalPlugin "github.com/realxen/cartograph/internal/plugin"
	"github.com/realxen/cartograph/plugin"
)

// BinaryResult holds the collected output from running a plugin binary.
// All mutation methods are thread-safe: notification handlers run concurrently.
type BinaryResult struct {
	// Info is the plugin's self-reported metadata.
	Info plugin.Info

	mu    sync.Mutex
	nodes []Node
	edges []Edge
	logs  []LogEntry
	errs  []error
}

// Nodes returns a copy of all emitted nodes.
func (r *BinaryResult) Nodes() []Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Node, len(r.nodes))
	copy(out, r.nodes)
	return out
}

// Edges returns a copy of all emitted edges.
func (r *BinaryResult) Edges() []Edge {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Edge, len(r.edges))
	copy(out, r.edges)
	return out
}

// Logs returns a copy of all log messages.
func (r *BinaryResult) Logs() []LogEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogEntry, len(r.logs))
	copy(out, r.logs)
	return out
}

// Errors returns any errors collected during the run.
func (r *BinaryResult) Errors() []error { return r.errs }

// AssertNodeCount fails the test if the number of emitted nodes doesn't match.
func (r *BinaryResult) AssertNodeCount(t testing.TB, expected int) {
	t.Helper()
	r.mu.Lock()
	got := len(r.nodes)
	r.mu.Unlock()
	if got != expected {
		t.Errorf("node count: got %d, want %d", got, expected)
	}
}

// AssertEdgeCount fails the test if the number of emitted edges doesn't match.
func (r *BinaryResult) AssertEdgeCount(t testing.TB, expected int) {
	t.Helper()
	r.mu.Lock()
	got := len(r.edges)
	r.mu.Unlock()
	if got != expected {
		t.Errorf("edge count: got %d, want %d", got, expected)
	}
}

// AssertNoErrors fails the test if any errors were collected.
func (r *BinaryResult) AssertNoErrors(t testing.TB) {
	t.Helper()
	if len(r.errs) > 0 {
		for _, err := range r.errs {
			t.Errorf("error: %v", err)
		}
	}
}

// AssertNodeExists fails the test if no emitted node matches the given ID and label.
func (r *BinaryResult) AssertNodeExists(t testing.TB, id, label string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.nodes {
		if n.ID == id && n.Label == label {
			return
		}
	}
	t.Errorf("node not found: id=%q label=%q", id, label)
}

// AssertEdgeExists fails the test if no emitted edge matches from→to with the given relType.
func (r *BinaryResult) AssertEdgeExists(t testing.TB, fromID, toID, relType string) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.edges {
		if e.FromID == fromID && e.ToID == toID && e.RelType == relType {
			return
		}
	}
	t.Errorf("edge not found: %q -[%s]-> %q", fromID, relType, toID)
}

// RunBinaryOptions configures RunBinary.
type RunBinaryOptions struct {
	// Config is the plugin configuration (same as sources.toml Extra fields).
	Config Config
	// Connection is the connection name passed to configure. Default: "test".
	Connection string
	// IngestOptions are passed to the ingest call.
	IngestOptions plugin.IngestOptions
	// Timeout is the maximum time for the entire run. Default: 30s.
	Timeout time.Duration
}

// RunBinary launches a compiled plugin binary as a subprocess, runs the
// full lifecycle (handshake → info → configure → ingest → close), and
// returns the collected emissions. This validates the complete protocol
// path including JSON-RPC serialization.
//
// The binary must be built before calling this. Use go build in TestMain
// or a build tag to ensure the binary exists.
func RunBinary(t testing.TB, binaryPath string, opts RunBinaryOptions) *BinaryResult {
	t.Helper()

	if opts.Connection == "" {
		opts.Connection = "test"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	result := &BinaryResult{}

	extra := make(map[string]any, len(opts.Config))
	for k, v := range opts.Config {
		extra[k] = v
	}

	builder := &collectingBuilder{result: result}

	ds := &internalPlugin.PluginDataSource{
		BinaryPath: binaryPath,
		SourceConfig: cloudgraph.SourceConfig{
			Type:  "test",
			Extra: extra,
		},
		ConnectionName: opts.Connection,
		Limits: internalPlugin.Limits{
			Timeout:  opts.Timeout,
			MaxNodes: -1, // unlimited for tests
			MaxEdges: -1,
		},
		Cache: internalPlugin.NewMemoryCache(),
		Logger: func(_ string, level string, msg string) {
			result.mu.Lock()
			defer result.mu.Unlock()
			result.logs = append(result.logs, LogEntry{Level: level, Msg: msg})
		},
	}

	info := ds.Info()
	result.Info = plugin.Info{
		Name:    info.Name,
		Version: info.Version,
	}
	types := ds.ResourceTypes()
	for _, rt := range types {
		result.Info.Resources = append(result.Info.Resources, plugin.Resource{
			Name:  rt.Name,
			Label: rt.Kind,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	ingestOpts := datasource.IngestOptions{
		ResourceTypes: opts.IngestOptions.ResourceTypes,
		Concurrency:   opts.IngestOptions.Concurrency,
	}
	if err := ds.Ingest(ctx, builder, ingestOpts); err != nil {
		result.errs = append(result.errs, err)
	}

	return result
}

// collectingBuilder is a datasource.GraphBuilder that records emissions
// into a BinaryResult. Thread-safe: notification handlers run concurrently.
type collectingBuilder struct {
	result *BinaryResult
}

func (b *collectingBuilder) AddNode(vendorLabel string, id string, properties map[string]any) {
	b.result.mu.Lock()
	defer b.result.mu.Unlock()
	b.result.nodes = append(b.result.nodes, Node{
		Label: vendorLabel,
		ID:    id,
		Props: properties,
	})
}

func (b *collectingBuilder) AddEdge(fromID string, toID string, relType string, properties map[string]any) {
	b.result.mu.Lock()
	defer b.result.mu.Unlock()
	b.result.edges = append(b.result.edges, Edge{
		FromID:  fromID,
		ToID:    toID,
		RelType: relType,
		Props:   properties,
	})
}

// MarshalNodeProps is a test helper that marshals a node's Props to JSON
// for snapshot comparison.
func MarshalNodeProps(n Node) string {
	data, err := json.MarshalIndent(n.Props, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(data)
}
